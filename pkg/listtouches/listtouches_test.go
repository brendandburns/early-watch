package listtouches

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	ewv1alpha1 "github.com/brendandburns/early-watch/pkg/apis/earlywatch/v1alpha1"
)

// makeEvent creates a ManualTouchEvent for testing.
func makeEvent(name, namespace, user, operation, resource, resourceName string, age time.Duration) ewv1alpha1.ManualTouchEvent {
	return ewv1alpha1.ManualTouchEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-age)),
		},
		Spec: ewv1alpha1.ManualTouchEventSpec{
			User:         user,
			Operation:    operation,
			Resource:     resource,
			ResourceName: resourceName,
		},
	}
}

// --- PrintTable tests ---

func TestPrintTable_EmptyList(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintTable(&buf, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "NAMESPACE") {
		t.Errorf("expected header row, got: %q", got)
	}
}

func TestPrintTable_SingleEvent(t *testing.T) {
	e := makeEvent("mte-1", "default", "admin", "DELETE", "services", "my-svc", 5*time.Minute)
	var buf bytes.Buffer
	if err := PrintTable(&buf, []ewv1alpha1.ManualTouchEvent{e}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"default", "mte-1", "admin", "DELETE", "services", "my-svc"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got: %q", want, got)
		}
	}
}

func TestPrintTable_MultipleEvents(t *testing.T) {
	events := []ewv1alpha1.ManualTouchEvent{
		makeEvent("mte-1", "default", "alice", "CREATE", "deployments", "app", time.Hour),
		makeEvent("mte-2", "kube-system", "bob", "DELETE", "pods", "my-pod", 2*time.Hour),
	}
	var buf bytes.Buffer
	if err := PrintTable(&buf, events); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"alice", "bob", "CREATE", "DELETE", "app", "my-pod"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got: %q", want, got)
		}
	}
}

// --- List tests ---

func TestList_AllNamespaces(t *testing.T) {
	s, err := BuildScheme()
	if err != nil {
		t.Fatalf("BuildScheme: %v", err)
	}

	e1 := makeEvent("mte-1", "ns-a", "alice", "DELETE", "services", "svc1", time.Minute)
	e2 := makeEvent("mte-2", "ns-b", "bob", "CREATE", "pods", "pod1", time.Hour)

	c := clientfake.NewClientBuilder().WithScheme(s).WithObjects(&e1, &e2).Build()

	list, err := List(context.Background(), c, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(list.Items))
	}
}

func TestList_SingleNamespace(t *testing.T) {
	s, err := BuildScheme()
	if err != nil {
		t.Fatalf("BuildScheme: %v", err)
	}

	e1 := makeEvent("mte-1", "ns-a", "alice", "DELETE", "services", "svc1", time.Minute)
	e2 := makeEvent("mte-2", "ns-b", "bob", "CREATE", "pods", "pod1", time.Hour)

	c := clientfake.NewClientBuilder().WithScheme(s).WithObjects(&e1, &e2).Build()

	list, err := List(context.Background(), c, "ns-a")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected 1 item for ns-a, got %d", len(list.Items))
	}
	if list.Items[0].Name != "mte-1" {
		t.Errorf("expected mte-1, got %q", list.Items[0].Name)
	}
}

func TestList_EmptyCluster(t *testing.T) {
	s, err := BuildScheme()
	if err != nil {
		t.Fatalf("BuildScheme: %v", err)
	}

	c := clientfake.NewClientBuilder().WithScheme(s).Build()

	list, err := List(context.Background(), c, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(list.Items))
	}
}

// --- formatAge tests ---

func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{90 * time.Minute, "1h"},
		{48 * time.Hour, "2d"},
		{-time.Second, "0s"},
	}
	for _, tc := range tests {
		got := formatAge(tc.d)
		if got != tc.want {
			t.Errorf("formatAge(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
