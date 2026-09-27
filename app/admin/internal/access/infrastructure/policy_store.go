package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/casbinrule"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/permissiondefinition"
	"github.com/eagle-go/eagle/pkg/authz"
)

type PolicyStore struct {
	client *ent.Client
}

func NewPolicyStore(db *platformdb.Database) *PolicyStore {
	return &PolicyStore{client: db.Client()}
}

func (s *PolicyStore) LoadPolicyRows(ctx context.Context) ([]authz.StoredPolicy, error) {
	rows, err := s.client.CasbinRule.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read policies: %w", err)
	}
	out := make([]authz.StoredPolicy, 0, len(rows))
	for _, row := range rows {
		out = append(out, authz.StoredPolicy{
			PType:  row.Ptype,
			Values: trimTrailingEmpty([]string{row.V0, row.V1, row.V2, row.V3, row.V4, row.V5}),
		})
	}
	return out, nil
}

func (s *PolicyStore) PolicyVersion(ctx context.Context) (int64, error) {
	state, err := s.client.PolicyState.Get(ctx, 1)
	if err != nil {
		return 0, fmt.Errorf("read policy version: %w", err)
	}
	return state.Version, nil
}

func (s *PolicyStore) PermissionCatalogCodes(ctx context.Context) ([]string, error) {
	rows, err := s.client.PermissionDefinition.Query().
		Where(permissiondefinition.StatusEQ(1)).
		Select(permissiondefinition.FieldCode).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read permission catalog: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Code)
	}
	return out, nil
}

type policyMutationMeta struct {
	actorSubject  string
	actorClientID string
	requestID     string
	traceID       string
}

func (s *PolicyStore) ReplaceRolePermissions(
	ctx context.Context,
	role string,
	perms []string,
	expected *int64,
	meta policyMutationMeta,
) (int64, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin role permission transaction: %w", err)
	}
	defer rollbackPolicyTxOnPanic(tx)
	if _, err := lockPolicyState(ctx, tx, expected); err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	oldRows, err := tx.CasbinRule.Query().
		Where(casbinrule.PtypeEQ("p"), casbinrule.V0EQ(role)).
		All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("read old permissions for %q: %w", role, err)
	}
	before := make([]string, 0, len(oldRows))
	for _, row := range oldRows {
		before = append(before, row.V1)
	}

	if _, err := tx.CasbinRule.Delete().
		Where(casbinrule.PtypeEQ("p"), casbinrule.V0EQ(role)).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("clear permissions for %q: %w", role, err)
	}
	if len(perms) > 0 {
		builders := make([]*ent.CasbinRuleCreate, 0, len(perms))
		for _, perm := range perms {
			builders = append(builders, newPolicyRule(tx.Client(), "p", []string{role, perm}))
		}
		if _, err := tx.CasbinRule.CreateBulk(builders...).Save(ctx); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("write permissions for %q: %w", role, err)
		}
	}

	version, err := recordPolicyMutation(ctx, tx, "replace_role_permissions", role, before, perms, meta)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit role permission transaction: %w", err)
	}
	return version, nil
}

func (s *PolicyStore) AddRoleInheritance(
	ctx context.Context,
	child, parent string,
	expected *int64,
	meta policyMutationMeta,
) (int64, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin role inheritance transaction: %w", err)
	}
	defer rollbackPolicyTxOnPanic(tx)
	state, err := lockPolicyState(ctx, tx, expected)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	grouping, err := tx.CasbinRule.Query().Where(casbinrule.PtypeEQ("g")).All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("read role inheritance graph: %w", err)
	}
	graph := make(map[string][]string)
	for _, rule := range grouping {
		graph[rule.V0] = append(graph[rule.V0], rule.V1)
	}
	if rolePathExists(graph, parent, child) {
		_ = tx.Rollback()
		return 0, domain.ErrRoleInheritanceCycle
	}

	exists, err := tx.CasbinRule.Query().Where(
		casbinrule.PtypeEQ("g"), casbinrule.V0EQ(child), casbinrule.V1EQ(parent),
	).Exist(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("check role inheritance: %w", err)
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit idempotent role inheritance: %w", err)
		}
		return state.Version, nil
	}
	if err := newPolicyRule(tx.Client(), "g", []string{child, parent}).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("write role inheritance: %w", err)
	}
	version, err := recordPolicyMutation(ctx, tx, "add_role_inheritance", child, nil, []string{parent}, meta)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit role inheritance: %w", err)
	}
	return version, nil
}

func (s *PolicyStore) DeleteRoleInheritance(
	ctx context.Context,
	child, parent string,
	expected *int64,
	meta policyMutationMeta,
) (int64, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin delete inheritance transaction: %w", err)
	}
	defer rollbackPolicyTxOnPanic(tx)
	state, err := lockPolicyState(ctx, tx, expected)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	deleted, err := tx.CasbinRule.Delete().Where(
		casbinrule.PtypeEQ("g"), casbinrule.V0EQ(child), casbinrule.V1EQ(parent),
	).Exec(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("delete role inheritance: %w", err)
	}
	if deleted == 0 {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit idempotent inheritance deletion: %w", err)
		}
		return state.Version, nil
	}
	version, err := recordPolicyMutation(ctx, tx, "delete_role_inheritance", child, []string{parent}, nil, meta)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit inheritance deletion: %w", err)
	}
	return version, nil
}

func stablePolicySnapshot[T any](
	ctx context.Context,
	store *PolicyStore,
	load func(context.Context) (T, error),
) (T, int64, error) {
	var zero T
	for range 5 {
		before, err := store.PolicyVersion(ctx)
		if err != nil {
			return zero, 0, err
		}
		value, err := load(ctx)
		if err != nil {
			return zero, 0, err
		}
		after, err := store.PolicyVersion(ctx)
		if err != nil {
			return zero, 0, err
		}
		if before == after {
			return value, after, nil
		}
	}
	return zero, 0, domain.ErrConcurrentModification
}

func (s *PolicyStore) RolePermissionsSnapshot(ctx context.Context, role string) ([]string, int64, error) {
	return stablePolicySnapshot(ctx, s, func(ctx context.Context) ([]string, error) {
		rows, err := s.client.CasbinRule.Query().
			Where(casbinrule.PtypeEQ("p"), casbinrule.V0EQ(role)).
			Select(casbinrule.FieldV1).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("read permissions for %q: %w", role, err)
		}
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.V1)
		}
		return out, nil
	})
}

func (s *PolicyStore) RulesSnapshot(ctx context.Context, ptype string) ([]authz.StoredPolicy, int64, error) {
	return stablePolicySnapshot(ctx, s, func(ctx context.Context) ([]authz.StoredPolicy, error) {
		rows, err := s.client.CasbinRule.Query().Where(casbinrule.PtypeEQ(ptype)).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("read %q policies: %w", ptype, err)
		}
		out := make([]authz.StoredPolicy, 0, len(rows))
		for _, row := range rows {
			out = append(out, authz.StoredPolicy{PType: row.Ptype, Values: []string{row.V0, row.V1}})
		}
		return out, nil
	})
}

func lockPolicyState(ctx context.Context, tx *ent.Tx, expected *int64) (*ent.PolicyState, error) {
	state, err := tx.PolicyState.UpdateOneID(1).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock policy state: %w", err)
	}
	if expected != nil && *expected != state.Version {
		return nil, domain.ErrConcurrentModification
	}
	return state, nil
}

func recordPolicyMutation(
	ctx context.Context,
	tx *ent.Tx,
	action, target string,
	before, after []string,
	meta policyMutationMeta,
) (int64, error) {
	state, err := tx.PolicyState.UpdateOneID(1).AddVersion(1).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("increment policy version: %w", err)
	}
	if _, err := tx.PolicyAudit.Create().
		SetPolicyVersion(state.Version).
		SetAction(action).
		SetTarget(target).
		SetActorSubject(meta.actorSubject).
		SetActorClientID(meta.actorClientID).
		SetRequestID(meta.requestID).
		SetTraceID(meta.traceID).
		SetBefore(before).
		SetAfter(after).
		Save(ctx); err != nil {
		return 0, fmt.Errorf("write policy audit: %w", err)
	}
	return state.Version, nil
}

func rolePathExists(graph map[string][]string, from, target string) bool {
	seen := make(map[string]struct{})
	stack := []string{from}
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current == target {
			return true
		}
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		stack = append(stack, graph[current]...)
	}
	return false
}

func newPolicyRule(client *ent.Client, ptype string, values []string) *ent.CasbinRuleCreate {
	v := make([]string, 6)
	copy(v, values)
	return client.CasbinRule.Create().
		SetPtype(ptype).
		SetV0(v[0]).SetV1(v[1]).SetV2(v[2]).
		SetV3(v[3]).SetV4(v[4]).SetV5(v[5])
}

func trimTrailingEmpty(values []string) []string {
	end := len(values)
	for end > 0 && values[end-1] == "" {
		end--
	}
	return values[:end]
}

func rollbackPolicyTxOnPanic(tx *ent.Tx) {
	if p := recover(); p != nil {
		_ = tx.Rollback()
		panic(p)
	}
}
