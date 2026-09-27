package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/app/admin/internal/dictionary/domain"
)

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
}

func TestDictRepoListByTypeReturnsEnabledItems(t *testing.T) {
	skipIfShort(t)
	items, err := NewDictRepo(testDB).ListDataByType(context.Background(), "sys_common_status")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("种子数据应包含通用状态字典项")
	}
	for _, item := range items {
		if !item.Status.Enabled() {
			t.Fatalf("返回了已停用字典项: %#v", item)
		}
	}
}

func TestDictRepoTypeCRUD(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()
	repo := NewDictRepo(testDB)
	created, err := repo.CreateType(ctx, &domain.DictType{Name: "测试字典", Type: "test_dict_crud", Status: domain.StatusEnabled})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.DeleteType(ctx, created.ID) })
	_, err = repo.CreateType(ctx, &domain.DictType{Name: "重复", Type: "test_dict_crud", Status: domain.StatusEnabled})
	if !errors.Is(err, domain.ErrDictTypeDuplicated) {
		t.Fatalf("重复类型错误 = %v", err)
	}
}

func TestDictRepoDataRejectsUnknownType(t *testing.T) {
	skipIfShort(t)
	_, err := NewDictRepo(testDB).CreateData(context.Background(), &domain.DictData{
		DictType: "no_such_dict_type", Label: "孤儿项", Value: "x", Status: domain.StatusEnabled,
	})
	if err == nil {
		t.Fatal("挂在不存在的字典类型下应失败")
	}
}
