// Package accessclient synchronizes the admin service's versioned policy
// snapshot and evaluates authorization locally on the request path.
package accessclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/pkg/authn"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/healthx"
	platformclient "github.com/eagle-go/eagle/pkg/platform/client"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/pkg/retryx"
)

type snapshotClient interface {
	GetPolicySnapshot(context.Context, *accessv1.GetPolicySnapshotRequest, ...grpc.CallOption) (*accessv1.GetPolicySnapshotResponse, error)
}

type Authorizer struct {
	client      snapshotClient
	enforcer    *authz.Enforcer
	maxAttempts int
	backoff     time.Duration
	lastRefresh atomic.Int64
}

func NewAuthorizer(c *config.Upstream, serviceAuth *config.ServiceAuth, logger *slog.Logger) (*Authorizer, func(), error) {
	credentials, err := authn.NewClientCredentials(authn.ClientCredentialsConfig{
		TokenURL: serviceAuth.GetTokenUrl(), ClientID: serviceAuth.GetClientId(), ClientSecret: serviceAuth.GetClientSecret(),
	})
	if err != nil {
		return nil, nil, err
	}
	middlewares, err := platformclient.NewMiddlewares(credentials.Client())
	if err != nil {
		return nil, nil, err
	}
	conn, err := kratosgrpc.NewClient(
		context.Background(),
		kratosgrpc.WithEndpoint(c.GetAuthorizationEndpoint()),
		kratosgrpc.WithTimeout(c.GetTimeout().AsDuration()),
		kratosgrpc.WithMiddleware(middlewares...),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect authorization service: %w", err)
	}
	enforcer, err := authz.NewEnforcer(nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	a := &Authorizer{
		client: accessv1.NewAuthorizationServiceClient(conn), enforcer: enforcer,
		maxAttempts: int(c.GetMaxAttempts()), backoff: c.GetRetryBackoff().AsDuration(),
	}
	if err := a.Refresh(context.Background()); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("load initial authorization policy: %w", err)
	}

	interval := c.GetAuthorizationRefreshInterval().AsDuration()
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := a.Refresh(ctx); err != nil && !errors.Is(err, context.Canceled) {
					logger.WarnContext(ctx, "authorization snapshot refresh failed; retaining last valid snapshot", "error", err, "loaded_version", enforcer.LoadedPolicyVersion())
				}
			}
		}
	}()
	maxStaleness := 3 * interval
	unregisterHealth := healthx.Default.Register("authorization-snapshot", func(context.Context) error {
		last := time.Unix(0, a.lastRefresh.Load())
		if time.Since(last) > maxStaleness {
			return fmt.Errorf("last successful refresh was %s ago", time.Since(last).Round(time.Second))
		}
		return nil
	})
	cleanup := func() {
		unregisterHealth()
		cancel()
		<-done
		_ = conn.Close()
	}
	return a, cleanup, nil
}

// Refresh fetches and atomically installs a complete snapshot. Older or equal
// versions never replace the current state, protecting against delayed replies.
func (a *Authorizer) Refresh(ctx context.Context) error {
	var resp *accessv1.GetPolicySnapshotResponse
	err := retryx.Do(ctx, a.maxAttempts, a.backoff, retryable, func() error {
		var err error
		resp, err = a.client.GetPolicySnapshot(ctx, &accessv1.GetPolicySnapshotRequest{})
		return err
	})
	if err != nil {
		return fmt.Errorf("fetch authorization policy snapshot: %w", err)
	}
	if resp.GetPolicyVersion() < 0 {
		return fmt.Errorf("authorization snapshot has invalid version %d", resp.GetPolicyVersion())
	}
	if resp.GetPolicyVersion() <= a.enforcer.LoadedPolicyVersion() && a.lastRefresh.Load() != 0 {
		a.lastRefresh.Store(time.Now().UnixNano())
		return nil
	}
	rules := make([]authz.StoredPolicy, 0, len(resp.GetRules()))
	for _, rule := range resp.GetRules() {
		if rule.GetPtype() != "p" && rule.GetPtype() != "g" {
			return fmt.Errorf("authorization snapshot contains unsupported policy type %q", rule.GetPtype())
		}
		if len(rule.GetValues()) != 2 {
			return fmt.Errorf("authorization snapshot rule %q requires exactly two values", rule.GetPtype())
		}
		rules = append(rules, authz.StoredPolicy{PType: rule.GetPtype(), Values: rule.GetValues()})
	}
	if err := a.enforcer.ReplacePolicySnapshot(ctx, rules, resp.GetPolicyVersion()); err != nil {
		return err
	}
	a.lastRefresh.Store(time.Now().UnixNano())
	return nil
}

func (a *Authorizer) AllowContext(ctx context.Context, roles []string, permission string) (bool, error) {
	return a.enforcer.AllowContext(ctx, roles, permission)
}

func retryable(err error) bool {
	code := status.Code(err)
	return code == codes.Unavailable || code == codes.ResourceExhausted
}

var _ interface {
	AllowContext(context.Context, []string, string) (bool, error)
} = (*Authorizer)(nil)
