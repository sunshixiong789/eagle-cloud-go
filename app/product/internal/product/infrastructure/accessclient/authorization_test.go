package accessclient

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/pkg/authz"
)

type stubSnapshotClient struct {
	response *accessv1.GetPolicySnapshotResponse
	err      error
	calls    int
}

func (s *stubSnapshotClient) GetPolicySnapshot(context.Context, *accessv1.GetPolicySnapshotRequest, ...grpc.CallOption) (*accessv1.GetPolicySnapshotResponse, error) {
	s.calls++
	return s.response, s.err
}

func newTestAuthorizer(t *testing.T, client snapshotClient) *Authorizer {
	t.Helper()
	enforcer, err := authz.NewEnforcer(nil)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	return &Authorizer{client: client, enforcer: enforcer, maxAttempts: 1}
}

func TestAuthorizerRefreshesSnapshotAndDecidesLocally(t *testing.T) {
	stub := &stubSnapshotClient{response: &accessv1.GetPolicySnapshotResponse{
		PolicyVersion: 7,
		Rules: []*accessv1.PolicyRule{
			{Ptype: "p", Values: []string{"realm:admin", "product:product:add"}},
		},
	}}
	authorizer := newTestAuthorizer(t, stub)
	if err := authorizer.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	allowed, err := authorizer.AllowContext(context.Background(), []string{"realm:admin"}, "product:product:add")
	if err != nil || !allowed {
		t.Fatalf("AllowContext = %v, %v", allowed, err)
	}
	if stub.calls != 1 {
		t.Fatalf("request path made a remote decision call: calls=%d", stub.calls)
	}
}

func TestAuthorizerRetainsLastValidSnapshotOnRefreshFailure(t *testing.T) {
	stub := &stubSnapshotClient{response: &accessv1.GetPolicySnapshotResponse{
		PolicyVersion: 1,
		Rules:         []*accessv1.PolicyRule{{Ptype: "p", Values: []string{"role", "product:product:list"}}},
	}}
	authorizer := newTestAuthorizer(t, stub)
	if err := authorizer.Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh: %v", err)
	}
	stub.err = errors.New("unavailable")
	if err := authorizer.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure should be reported")
	}
	allowed, err := authorizer.AllowContext(context.Background(), []string{"role"}, "product:product:list")
	if err != nil || !allowed {
		t.Fatalf("last valid snapshot was not retained: %v, %v", allowed, err)
	}
}

func TestAuthorizerRejectsMalformedSnapshotWithoutReplacingPolicy(t *testing.T) {
	stub := &stubSnapshotClient{response: &accessv1.GetPolicySnapshotResponse{
		PolicyVersion: 1,
		Rules:         []*accessv1.PolicyRule{{Ptype: "p", Values: []string{"role", "product:product:list"}}},
	}}
	authorizer := newTestAuthorizer(t, stub)
	if err := authorizer.Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh: %v", err)
	}
	stub.response = &accessv1.GetPolicySnapshotResponse{
		PolicyVersion: 2,
		Rules:         []*accessv1.PolicyRule{{Ptype: "x", Values: []string{"role", "anything"}}},
	}
	if err := authorizer.Refresh(context.Background()); err == nil {
		t.Fatal("malformed snapshot should fail")
	}
	allowed, err := authorizer.AllowContext(context.Background(), []string{"role"}, "product:product:list")
	if err != nil || !allowed {
		t.Fatalf("malformed snapshot replaced valid policy: %v, %v", allowed, err)
	}
}
