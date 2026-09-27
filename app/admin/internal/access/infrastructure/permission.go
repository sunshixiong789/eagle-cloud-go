package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/permission"
	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/permissiondefinition"
)

type permissionRepo struct {
	db *platformdb.Database
}

func NewPermissionRepo(db *platformdb.Database) domain.PermissionRepo {
	return &permissionRepo{db: db}
}

func toDomainPermission(p *ent.Permission, revision int64) (*domain.Permission, error) {
	if p == nil {
		return nil, nil
	}
	parentID := domain.RootPermissionID
	if p.ParentID != nil {
		parentID = *p.ParentID
	}
	code := ""
	if p.Code != nil {
		code = *p.Code
	}
	return domain.RehydratePermission(domain.PermissionSnapshot{
		ID: p.ID, ParentID: parentID, Name: p.Name, Code: code,
		Type: p.Type, Status: p.Status, Path: p.Path, Component: p.Component,
		Icon: p.Icon, Sort: p.Sort, Visible: p.Visible,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Revision: revision,
	})
}

func (r *permissionRepo) withTreeTx(ctx context.Context, expected *int64, fn func(*ent.Tx, *ent.PermissionTreeState) error) error {
	tx, err := r.db.Client().Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin permission tx: %w", err)
	}
	defer rollbackPermissionTxOnPanic(tx)

	state, err := lockPermissionTree(ctx, tx, expected)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := fn(tx, state); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit permission tx: %w", err)
	}
	return nil
}

func (r *permissionRepo) Create(ctx context.Context, p *domain.Permission) (*domain.Permission, error) {
	var created *domain.Permission
	err := r.withTreeTx(ctx, nil, func(tx *ent.Tx, state *ent.PermissionTreeState) error {
		if !p.IsRoot() {
			exists, err := tx.Permission.Query().Where(permission.IDEQ(p.ParentID())).Exist(ctx)
			if err != nil {
				return fmt.Errorf("check permission parent: %w", err)
			}
			if !exists {
				return domain.ErrPermissionNotFound
			}
		}
		if err := requirePermissionDefinition(ctx, tx, p.Code()); err != nil {
			return err
		}
		row, err := tx.Permission.Create().
			SetNillableParentID(permissionParentPtr(p.ParentID())).
			SetName(p.Name()).
			SetNillableCode(permissionCodePtr(p.Code())).
			SetType(int32(p.Type())).
			SetPath(p.Path()).
			SetComponent(p.Component()).
			SetIcon(p.Icon()).
			SetSort(p.Sort()).
			SetVisible(p.Visible()).
			SetStatus(int32(p.Status())).
			Save(ctx)
		if err != nil {
			if platformdb.IsUniqueViolation(err) {
				return domain.ErrPermissionCodeDuplicated
			}
			return fmt.Errorf("create permission: %w", err)
		}
		next, err := bumpPermissionTreeRevision(ctx, tx, state.Revision)
		if err != nil {
			return err
		}
		created, err = toDomainPermission(row, next)
		return err
	})
	return created, err
}

func (r *permissionRepo) GetByID(ctx context.Context, id int64) (*domain.Permission, error) {
	revision, err := r.currentRevision(ctx)
	if err != nil {
		return nil, err
	}
	p, err := r.db.Client().Permission.Get(ctx, id)
	if err != nil {
		if platformdb.IsNotFound(err) {
			return nil, domain.ErrPermissionNotFound
		}
		return nil, fmt.Errorf("get permission %d: %w", id, err)
	}
	return toDomainPermission(p, revision)
}

// List 平铺返回权限。总量只有百级，一次全量取出比递归 CTE 更简单也更快。
func (r *permissionRepo) List(ctx context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
	revision, err := r.currentRevision(ctx)
	if err != nil {
		return nil, err
	}
	query := r.db.Client().Permission.Query()
	if q.Status != nil {
		query = query.Where(permission.StatusEQ(int32(*q.Status)))
	}
	if q.Type != nil {
		query = query.Where(permission.TypeEQ(int32(*q.Type)))
	}

	rows, err := query.
		Order(ent.Asc(permission.FieldParentID), ent.Asc(permission.FieldSort), ent.Asc(permission.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}

	out := make([]*domain.Permission, 0, len(rows))
	for _, row := range rows {
		value, err := toDomainPermission(row, revision)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func (r *permissionRepo) Update(ctx context.Context, p *domain.Permission, expectedRevision *int64) (*domain.Permission, error) {
	var updated *domain.Permission
	err := r.withTreeTx(ctx, expectedRevision, func(tx *ent.Tx, state *ent.PermissionTreeState) error {
		rows, err := tx.Permission.Query().All(ctx)
		if err != nil {
			return fmt.Errorf("load permission tree for update: %w", err)
		}
		perms := make([]*domain.Permission, 0, len(rows))
		for _, row := range rows {
			value, err := toDomainPermission(row, state.Revision)
			if err != nil {
				return err
			}
			perms = append(perms, value)
		}
		if err := domain.NewPermissionTree(perms).EnsureNoCycle(p.ID(), p.ParentID()); err != nil {
			return err
		}
		if err := requirePermissionDefinition(ctx, tx, p.Code()); err != nil {
			return err
		}
		row, err := tx.Permission.UpdateOneID(p.ID()).
			SetNillableParentID(permissionParentPtr(p.ParentID())).
			SetName(p.Name()).
			SetNillableCode(permissionCodePtr(p.Code())).
			SetType(int32(p.Type())).
			SetPath(p.Path()).
			SetComponent(p.Component()).
			SetIcon(p.Icon()).
			SetSort(p.Sort()).
			SetVisible(p.Visible()).
			SetStatus(int32(p.Status())).
			Save(ctx)
		if err != nil {
			if platformdb.IsNotFound(err) {
				return domain.ErrPermissionNotFound
			}
			if platformdb.IsUniqueViolation(err) {
				return domain.ErrPermissionCodeDuplicated
			}
			return fmt.Errorf("update permission %d: %w", p.ID(), err)
		}
		next, err := bumpPermissionTreeRevision(ctx, tx, state.Revision)
		if err != nil {
			return err
		}
		updated, err = toDomainPermission(row, next)
		return err
	})
	return updated, err
}

func (r *permissionRepo) Delete(ctx context.Context, id int64, expectedRevision *int64) error {
	return r.withTreeTx(ctx, expectedRevision, func(tx *ent.Tx, state *ent.PermissionTreeState) error {
		children, err := tx.Permission.Query().Where(permission.ParentIDEQ(id)).Count(ctx)
		if err != nil {
			return fmt.Errorf("count children of permission %d: %w", id, err)
		}
		if children > 0 {
			return domain.ErrPermissionHasChildren
		}
		if err := tx.Permission.DeleteOneID(id).Exec(ctx); err != nil {
			if platformdb.IsNotFound(err) {
				return domain.ErrPermissionNotFound
			}
			return fmt.Errorf("delete permission %d: %w", id, err)
		}
		_, err = bumpPermissionTreeRevision(ctx, tx, state.Revision)
		return err
	})
}

func permissionParentPtr(parentID int64) *int64 {
	if parentID == domain.RootPermissionID {
		return nil
	}
	return &parentID
}

func permissionCodePtr(code domain.PermissionCode) *string {
	if code.IsZero() {
		return nil
	}
	value := code.String()
	return &value
}

// requirePermissionDefinition 要求导航节点引用的权限码已存在于目录。
// 目录由迁移/种子写入，与 proto 注解对齐；创建菜单不能发明新契约。
func requirePermissionDefinition(ctx context.Context, tx *ent.Tx, code domain.PermissionCode) error {
	if code.IsZero() {
		return nil
	}
	exists, err := tx.PermissionDefinition.Query().
		Where(
			permissiondefinition.CodeEQ(code.String()),
			permissiondefinition.StatusEQ(1),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("lookup permission definition %q: %w", code, err)
	}
	if !exists {
		return fmt.Errorf("%w: %s", domain.ErrUnknownPermissionCode, code)
	}
	return nil
}

func (r *permissionRepo) currentRevision(ctx context.Context) (int64, error) {
	state, err := r.db.Client().PermissionTreeState.Get(ctx, 1)
	if err != nil {
		return 0, fmt.Errorf("get permission tree revision: %w", err)
	}
	return state.Revision, nil
}

func (r *permissionRepo) KnownCodes(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.db.Client().PermissionDefinition.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list permission definitions: %w", err)
	}
	out := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.Status == 1 {
			out[row.Code] = struct{}{}
		}
	}
	return out, nil
}

func lockPermissionTree(ctx context.Context, tx *ent.Tx, expected *int64) (*ent.PermissionTreeState, error) {
	// UPDATE 即使只改 updated_at 也会取得该单例行的排他锁，使所有树写入串行。
	state, err := tx.PermissionTreeState.UpdateOneID(1).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock permission tree: %w", err)
	}
	if expected != nil && *expected != state.Revision {
		return nil, domain.ErrConcurrentModification
	}
	return state, nil
}

func bumpPermissionTreeRevision(ctx context.Context, tx *ent.Tx, current int64) (int64, error) {
	state, err := tx.PermissionTreeState.UpdateOneID(1).SetRevision(current + 1).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("bump permission tree revision: %w", err)
	}
	return state.Revision, nil
}

func rollbackPermissionTxOnPanic(tx *ent.Tx) {
	if p := recover(); p != nil {
		_ = tx.Rollback()
		panic(p)
	}
}
