package auditmonitor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)

// defaultUserAgentPatterns is the list of regular expressions used when no
// patterns are configured in a ManualTouchMonitor.  Any request whose
// User-Agent header starts with "kubectl/" is treated as a manual touch.
var defaultUserAgentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^kubectl/`),
}

// monitoredVerbs is the set of Kubernetes audit verbs (lower-cased) that can
// constitute a manual touch.
var monitoredVerbs = map[string]ewv1alpha1.MonitorOperationType{
	"delete":           ewv1alpha1.MonitorOperationDelete,
	"deletecollection": ewv1alpha1.MonitorOperationDelete, // DELETECOLLECTION maps to DELETE
	"create":           ewv1alpha1.MonitorOperationCreate,
	"update":           ewv1alpha1.MonitorOperationUpdate,
	"patch":            ewv1alpha1.MonitorOperationUpdate, // PATCH maps to UPDATE
}

// patternCacheKey uniquely identifies a compiled pattern set for a monitor.
type patternCacheKey struct {
	name       string
	namespace  string
	generation int64
}

// namespaceCacheEntry stores the labels for a Namespace with an expiry time.
type namespaceCacheEntry struct {
	labels    map[string]string
	expiresAt time.Time
}

// namespaceCacheTTL is how long a cached namespace entry is considered fresh.
const namespaceCacheTTL = 30 * time.Second

// patternCacheEntry stores the compiled result for a monitor's patterns.
// valid is false when all configured patterns were invalid.
type patternCacheEntry struct {
	patterns []*regexp.Regexp
	valid    bool
}

// patternCache caches compiled regexp slices keyed by monitor identity +
// generation so patterns are only recompiled when the monitor spec changes.
var (
	patternCacheMu sync.RWMutex
	patternCache   = map[patternCacheKey]patternCacheEntry{}
)

// TouchRecord holds the information needed to create a ManualTouchEvent.
type TouchRecord struct {
	Event            *AuditEvent
	Operation        ewv1alpha1.MonitorOperationType
	MonitorName      string
	MonitorNamespace string
}

// TouchDetector checks each audit event against all ManualTouchMonitor
// resources in the cluster and returns a TouchRecord for every match.
type TouchDetector struct {
	Client client.Client

	// nsCache is an in-process cache of namespace labels keyed by namespace name.
	// Guarded by nsCacheMu.
	nsCache   map[string]namespaceCacheEntry
	nsCacheMu sync.RWMutex
}

// Detect evaluates a single audit event against all ManualTouchMonitor
// resources and returns a TouchRecord for each monitor that matches.
func (d *TouchDetector) Detect(ctx context.Context, event *AuditEvent) ([]TouchRecord, error) {
	logger := log.FromContext(ctx).WithValues("auditID", event.AuditID)

	// Map the event verb to a MonitorOperationType.
	op, ok := monitoredVerbs[strings.ToLower(event.Verb)]
	if !ok {
		return nil, nil
	}

	// List all ManualTouchMonitors across all namespaces.
	monitorList := &ewv1alpha1.ManualTouchMonitorList{}
	if err := d.Client.List(ctx, monitorList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("listing ManualTouchMonitors: %w", err)
	}

	records := make([]TouchRecord, 0, len(monitorList.Items))

	for i := range monitorList.Items {
		monitor := &monitorList.Items[i]

		if !d.monitorMatchesEvent(ctx, monitor, event, op) {
			continue
		}

		logger.Info("Manual touch detected",
			"monitor", monitor.Name,
			"user", event.User.Username,
			"resource", event.ObjectRef.Resource,
			"name", event.ObjectRef.Name,
		)

		records = append(records, TouchRecord{
			Event:            event,
			Operation:        op,
			MonitorName:      monitor.Name,
			MonitorNamespace: monitor.Namespace,
		})
	}

	return records, nil
}

// monitorMatchesEvent returns true when the audit event matches the monitor's
// configured subjects, operations, user-agent patterns, and service-account
// exclusions.
func (d *TouchDetector) monitorMatchesEvent(
	ctx context.Context,
	monitor *ewv1alpha1.ManualTouchMonitor,
	event *AuditEvent,
	op ewv1alpha1.MonitorOperationType,
) bool {
	// Check operation.
	if !operationMatches(monitor.Spec.Operations, op) {
		return false
	}

	// Check user-agent using cached compiled patterns.
	patterns, ok := cachedPatterns(monitor)
	if !ok {
		// All configured patterns were invalid — treat as non-matching.
		return false
	}
	if !userAgentMatches(event.UserAgent, patterns) {
		return false
	}

	// Check service-account exclusions.
	if isExcluded(event.User.Username, monitor.Spec.ExcludeServiceAccounts) {
		return false
	}

	// Check subjects.
	for _, subj := range monitor.Spec.Subjects {
		if d.subjectMatchesEvent(ctx, subj, event) {
			return true
		}
	}

	return false
}

// operationMatches returns true when op is in the allowed list.
func operationMatches(ops []ewv1alpha1.MonitorOperationType, op ewv1alpha1.MonitorOperationType) bool {
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

// cachedPatterns returns the compiled regexp slice for the monitor, building
// and caching it when necessary.  The second return value is false when all
// configured patterns are invalid (the caller should treat the monitor as
// non-matching until the configuration is corrected).
func cachedPatterns(monitor *ewv1alpha1.ManualTouchMonitor) ([]*regexp.Regexp, bool) {
	// No patterns configured — use the default kubectl pattern.
	if len(monitor.Spec.UserAgentPatterns) == 0 {
		return defaultUserAgentPatterns, true
	}

	key := patternCacheKey{
		name:       monitor.Name,
		namespace:  monitor.Namespace,
		generation: monitor.Generation,
	}

	// Fast path: check cache with read lock.
	patternCacheMu.RLock()
	if entry, hit := patternCache[key]; hit {
		patternCacheMu.RUnlock()
		return entry.patterns, entry.valid
	}
	patternCacheMu.RUnlock()

	// Slow path: compile and cache with write lock.
	patternCacheMu.Lock()
	defer patternCacheMu.Unlock()

	// Re-check after acquiring write lock (another goroutine may have populated it).
	if entry, hit := patternCache[key]; hit {
		return entry.patterns, entry.valid
	}

	compiled := make([]*regexp.Regexp, 0, len(monitor.Spec.UserAgentPatterns))
	for _, p := range monitor.Spec.UserAgentPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		compiled = append(compiled, re)
	}

	if len(compiled) == 0 {
		// All patterns were invalid — store explicit invalid entry.
		patternCache[key] = patternCacheEntry{patterns: nil, valid: false}
		return nil, false
	}

	patternCache[key] = patternCacheEntry{patterns: compiled, valid: true}
	return compiled, true
}

// userAgentMatches returns true when the user agent matches at least one
// compiled pattern.
func userAgentMatches(userAgent string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(userAgent) {
			return true
		}
	}
	return false
}

// isExcluded returns true when username is present in the exclusion list.
func isExcluded(username string, exclusions []string) bool {
	for _, ex := range exclusions {
		if ex == username {
			return true
		}
	}
	return false
}

// subjectMatchesEvent returns true when the monitor subject matches the
// resource referenced in the audit event.
func (d *TouchDetector) subjectMatchesEvent(ctx context.Context, subj ewv1alpha1.MonitorSubject, event *AuditEvent) bool {
	// Match API group.
	if subj.APIGroup != event.ObjectRef.APIGroup {
		return false
	}

	// Match resource (case-insensitive).
	if !strings.EqualFold(subj.Resource, event.ObjectRef.Resource) {
		return false
	}

	// Evaluate NamespaceSelector against the namespace's actual labels.
	if subj.NamespaceSelector != nil {
		if !d.namespaceMatchesSelector(ctx, event.ObjectRef.Namespace, subj.NamespaceSelector) {
			return false
		}
	}

	return true
}

// namespaceMatchesSelector fetches the Namespace object (with a short-TTL
// in-memory cache) and evaluates the LabelSelector against its labels,
// implementing full Kubernetes label selector semantics (matchLabels +
// matchExpressions).
func (d *TouchDetector) namespaceMatchesSelector(ctx context.Context, namespace string, sel *metav1.LabelSelector) bool {
	if sel == nil {
		return true
	}
	if len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0 {
		return true
	}

	// Convert the metav1.LabelSelector to a labels.Selector.
	selector, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		// Invalid selector — treat as non-matching.
		return false
	}

	nsLabels := d.namespaceLabels(ctx, namespace)
	if nsLabels == nil {
		return false
	}

	return selector.Matches(labels.Set(nsLabels))
}

// namespaceLabels returns the labels of the named Namespace, using a short-TTL
// cache to avoid an API call for every audit event.  Returns nil on error.
func (d *TouchDetector) namespaceLabels(ctx context.Context, namespace string) map[string]string {
	now := time.Now()

	// Fast path: read from cache.
	d.nsCacheMu.RLock()
	if d.nsCache != nil {
		if entry, hit := d.nsCache[namespace]; hit && now.Before(entry.expiresAt) {
			d.nsCacheMu.RUnlock()
			return entry.labels
		}
	}
	d.nsCacheMu.RUnlock()

	// Slow path: fetch from API.
	ns := &corev1.Namespace{}
	if err := d.Client.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		return nil
	}

	d.nsCacheMu.Lock()
	if d.nsCache == nil {
		d.nsCache = make(map[string]namespaceCacheEntry)
	}
	d.nsCache[namespace] = namespaceCacheEntry{
		labels:    ns.Labels,
		expiresAt: now.Add(namespaceCacheTTL),
	}
	d.nsCacheMu.Unlock()

	return ns.Labels
}
