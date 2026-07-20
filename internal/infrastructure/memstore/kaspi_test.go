package memstore_test

import (
	"testing"

	"github.com/vevovip/chaospay/internal/domain/kaspi"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

func TestKaspiRepo_CreateGet(t *testing.T) {
	t.Parallel()

	repo := memstore.NewKaspiRepo()

	p := repo.Create("order-1", 1790)
	if p.PaymentID == 0 {
		t.Fatal("Create returned zero PaymentID")
	}
	if p.Status != kaspi.StatusWait {
		t.Fatalf("new payment status = %q, want Wait", p.Status)
	}
	if p.ExternalID != "order-1" || p.Amount != 1790 {
		t.Fatalf("unexpected payment fields: %+v", p)
	}

	got, err := repo.Get(p.PaymentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PaymentID != p.PaymentID {
		t.Fatalf("Get returned id %d, want %d", got.PaymentID, p.PaymentID)
	}
}

func TestKaspiRepo_GetNotFound(t *testing.T) {
	t.Parallel()

	repo := memstore.NewKaspiRepo()
	if _, err := repo.Get(123); err != memstore.ErrKaspiNotFound {
		t.Fatalf("Get(missing) err = %v, want ErrKaspiNotFound", err)
	}
}

func TestKaspiRepo_SetStatus(t *testing.T) {
	t.Parallel()

	repo := memstore.NewKaspiRepo()
	p := repo.Create("order-2", 500)

	updated, err := repo.SetStatus(p.PaymentID, kaspi.StatusProcessed)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if updated.Status != kaspi.StatusProcessed {
		t.Fatalf("status after SetStatus = %q, want Processed", updated.Status)
	}

	got, _ := repo.Get(p.PaymentID)
	if got.Status != kaspi.StatusProcessed {
		t.Fatalf("persisted status = %q, want Processed", got.Status)
	}

	if _, err := repo.SetStatus(999999, kaspi.StatusError); err != memstore.ErrKaspiNotFound {
		t.Fatalf("SetStatus(missing) err = %v, want ErrKaspiNotFound", err)
	}
}

func TestKaspiRepo_ListNewestFirst(t *testing.T) {
	t.Parallel()

	repo := memstore.NewKaspiRepo()
	first := repo.Create("a", 1)
	second := repo.Create("b", 2)

	list := repo.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	if list[0].PaymentID != second.PaymentID || list[1].PaymentID != first.PaymentID {
		t.Fatalf("List order wrong: got %d,%d want %d,%d",
			list[0].PaymentID, list[1].PaymentID, second.PaymentID, first.PaymentID)
	}
}

func TestKaspiRepo_GetReturnsCopy(t *testing.T) {
	t.Parallel()

	repo := memstore.NewKaspiRepo()
	p := repo.Create("order-3", 100)

	got, _ := repo.Get(p.PaymentID)
	got.Status = kaspi.StatusError // мутация копии не должна влиять на стор

	fresh, _ := repo.Get(p.PaymentID)
	if fresh.Status != kaspi.StatusWait {
		t.Fatalf("store mutated via returned copy: status = %q", fresh.Status)
	}
}
