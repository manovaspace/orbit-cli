package domain

import (
	"errors"
	"testing"
	"time"
)

// Tier-1 invariant 3 (admin grant 3-strike lockout): the third failed attempt
// burns the grant, and the correct code is rejected afterwards. Covers the
// in-memory manager and the store-backed (persistent) manager.
func TestGrantBurnInvariantThirdStrikeRejectsCorrectCode(t *testing.T) {
	managers := []struct {
		name string
		new  func() *GrantManager
	}{
		{name: "in-memory", new: NewGrantManager},
		{name: "persistent", new: func() *GrantManager { return NewPersistentGrantManager(newMockGrantStore()) }},
	}

	for _, mc := range managers {
		t.Run(mc.name, func(t *testing.T) {
			gm := mc.new()
			email := "burn-" + mc.name + "@example.test"

			_, code, err := gm.CreateGrant(email, "admin", "root@example.test", 10*time.Minute)
			if err != nil {
				t.Fatalf("CreateGrant: %v", err)
			}
			wrong := "00000000"
			if CleanCode(code) == wrong {
				wrong = "11111111"
			}

			for strike := 1; strike <= 2; strike++ {
				rec, err := gm.VerifyGrant(email, wrong)
				if !errors.Is(err, ErrInvalidGrantCode) || rec != nil {
					t.Fatalf("strike %d: got rec=%v err=%v, want ErrInvalidGrantCode", strike, rec, err)
				}
			}

			rec, err := gm.VerifyGrant(email, wrong)
			if !errors.Is(err, ErrGrantMaxAttempts) || rec != nil {
				t.Fatalf("strike 3: got rec=%v err=%v, want ErrGrantMaxAttempts", rec, err)
			}

			rec, err = gm.VerifyGrant(email, code)
			if err == nil || rec != nil {
				t.Fatalf("correct code after burn: got rec=%v err=%v, want rejection", rec, err)
			}
			// The grant is either purged (in-memory) or kept as used (store-backed);
			// it must never be reported as redeemable.
			if got, ok := gm.GetGrant(email); ok && !got.Used {
				t.Errorf("grant still redeemable after third strike: %+v", got)
			}
		})
	}
}
