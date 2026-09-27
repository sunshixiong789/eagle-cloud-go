package infrastructure

import (
	"context"
	"testing"
	"time"

	"github.com/eagle-go/eagle/app/admin/internal/platform/database/ent/casbinrule"
)

func TestPolicyReconcilerSyncsAcrossReplicas(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	const role = "realm:test-multi-replica-role"
	const permCode = "system:dict:add"

	t.Cleanup(func() {
		_, _ = testDB.Client().CasbinRule.Delete().
			Where(casbinrule.V0EQ(role)).Exec(ctx)
	})

	// 副本 B 先加载。此刻数据库里还没有这条策略。
	store := NewPolicyStore(testDB)
	enforcerB, err := NewEnforcer(store)
	if err != nil {
		t.Fatalf("构造副本 B 的 enforcer: %v", err)
	}
	t.Cleanup(NewPolicyReconciler(store, enforcerB, nil))

	if ok, err := enforcerB.Allow([]string{role}, permCode); err != nil {
		t.Fatalf("Allow(写入前): %v", err)
	} else if ok {
		t.Fatal("写入前不应已经放行")
	}

	// 副本 A 写一次；副本 B 只靠数据库版本对账追平。
	enforcerA, err := NewEnforcer(store)
	if err != nil {
		t.Fatalf("构造副本 A 的 enforcer: %v", err)
	}
	storeA := NewPolicyRepo(enforcerA, store)
	if _, err := storeA.SaveBinding(ctx, mustBinding(t, role, permCode), nil); err != nil {
		t.Fatalf("副本 A SaveBinding: %v", err)
	}

	deadline := time.Now().Add(7 * time.Second)
	for {
		ok, err := enforcerB.Allow([]string{role}, permCode)
		if err != nil {
			t.Fatalf("Allow(等待同步): %v", err)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("超时：副本 B 未通过版本对账同步到副本 A 写入的策略")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
