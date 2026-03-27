package mempool

import (
	"fmt"
	"testing"

	"github.com/Bihan293/Noda/block"
)

func makeTx(id string, from, to string, amount float64) block.Transaction {
	return block.Transaction{
		ID:        id,
		From:      from,
		To:        to,
		Amount:    amount,
		Timestamp: 1000,
		Signature: "sig",
	}
}

func TestNew(t *testing.T) {
	mp := New(100)
	if mp == nil {
		t.Fatal("New() returned nil")
	}
	if mp.Size() != 0 {
		t.Errorf("new mempool Size() = %d, want 0", mp.Size())
	}
}

func TestNew_DefaultSize(t *testing.T) {
	mp := New(0)
	if mp.maxSize != DefaultMaxSize {
		t.Errorf("New(0).maxSize = %d, want %d", mp.maxSize, DefaultMaxSize)
	}
}

func TestAdd(t *testing.T) {
	mp := New(100)
	tx := makeTx("tx1", "alice", "bob", 50)

	err := mp.Add(tx)
	if err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if mp.Size() != 1 {
		t.Errorf("Size() = %d after Add, want 1", mp.Size())
	}
}

func TestAdd_Duplicate(t *testing.T) {
	mp := New(100)
	tx := makeTx("tx1", "alice", "bob", 50)

	mp.Add(tx)
	err := mp.Add(tx)
	if err == nil {
		t.Error("Add() should fail for duplicate transaction")
	}
}

func TestAdd_NoID(t *testing.T) {
	mp := New(100)
	tx := makeTx("", "alice", "bob", 50)

	err := mp.Add(tx)
	if err == nil {
		t.Error("Add() should fail for transaction without ID")
	}
}

func TestAdd_PoolFull(t *testing.T) {
	mp := New(2)

	mp.Add(makeTx("tx1", "a", "b", 1))
	mp.Add(makeTx("tx2", "a", "b", 2))

	// Third should evict the oldest.
	err := mp.Add(makeTx("tx3", "a", "b", 3))
	if err != nil {
		t.Fatalf("Add() should evict oldest when full, got error: %v", err)
	}
	if mp.Size() != 2 {
		t.Errorf("Size() = %d after eviction, want 2", mp.Size())
	}
	// tx1 should have been evicted.
	if mp.Has("tx1") {
		t.Error("tx1 should have been evicted")
	}
}

func TestRemove(t *testing.T) {
	mp := New(100)
	tx := makeTx("tx1", "alice", "bob", 50)
	mp.Add(tx)

	mp.Remove("tx1")
	if mp.Size() != 0 {
		t.Errorf("Size() = %d after Remove, want 0", mp.Size())
	}
	if mp.Has("tx1") {
		t.Error("Has(tx1) should be false after Remove")
	}
}

func TestRemoveBatch(t *testing.T) {
	mp := New(100)
	mp.Add(makeTx("tx1", "a", "b", 1))
	mp.Add(makeTx("tx2", "a", "b", 2))
	mp.Add(makeTx("tx3", "a", "b", 3))

	mp.RemoveBatch([]string{"tx1", "tx3"})
	if mp.Size() != 1 {
		t.Errorf("Size() = %d after RemoveBatch, want 1", mp.Size())
	}
	if !mp.Has("tx2") {
		t.Error("tx2 should still be in pool")
	}
}

func TestHas(t *testing.T) {
	mp := New(100)
	tx := makeTx("tx1", "alice", "bob", 50)
	mp.Add(tx)

	if !mp.Has("tx1") {
		t.Error("Has(tx1) = false, want true")
	}
	if mp.Has("nonexistent") {
		t.Error("Has(nonexistent) = true, want false")
	}
}

func TestGet(t *testing.T) {
	mp := New(100)
	tx := makeTx("tx1", "alice", "bob", 50)
	mp.Add(tx)

	got := mp.Get("tx1")
	if got == nil {
		t.Fatal("Get(tx1) returned nil")
	}
	if got.ID != "tx1" {
		t.Errorf("Get(tx1).ID = %s, want tx1", got.ID)
	}

	if mp.Get("nonexistent") != nil {
		t.Error("Get(nonexistent) should return nil")
	}
}

func TestGetPending(t *testing.T) {
	mp := New(100)
	for i := 0; i < 5; i++ {
		mp.Add(makeTx(fmt.Sprintf("tx%d", i), "a", "b", float64(i)))
	}

	// Get only 3.
	pending := mp.GetPending(3)
	if len(pending) != 3 {
		t.Errorf("GetPending(3) len = %d, want 3", len(pending))
	}

	// Get all.
	all := mp.GetAll()
	if len(all) != 5 {
		t.Errorf("GetAll() len = %d, want 5", len(all))
	}
}

func TestGetPending_FIFO(t *testing.T) {
	mp := New(100)
	mp.Add(makeTx("tx1", "a", "b", 1))
	mp.Add(makeTx("tx2", "a", "b", 2))
	mp.Add(makeTx("tx3", "a", "b", 3))

	pending := mp.GetPending(2)
	if pending[0].ID != "tx1" {
		t.Errorf("first pending = %s, want tx1", pending[0].ID)
	}
	if pending[1].ID != "tx2" {
		t.Errorf("second pending = %s, want tx2", pending[1].ID)
	}
}

func TestHasSpendFrom(t *testing.T) {
	mp := New(100)
	mp.Add(makeTx("tx1", "alice", "bob", 50))

	if !mp.HasSpendFrom("alice") {
		t.Error("HasSpendFrom(alice) = false, want true")
	}
	if mp.HasSpendFrom("bob") {
		t.Error("HasSpendFrom(bob) = true, want false")
	}
}

func TestGetSpendingTotal(t *testing.T) {
	mp := New(100)
	mp.Add(makeTx("tx1", "alice", "bob", 30))
	mp.Add(makeTx("tx2", "alice", "charlie", 20))
	mp.Add(makeTx("tx3", "bob", "alice", 10))

	total := mp.GetSpendingTotal("alice")
	if total != 50 {
		t.Errorf("GetSpendingTotal(alice) = %f, want 50", total)
	}

	total = mp.GetSpendingTotal("bob")
	if total != 10 {
		t.Errorf("GetSpendingTotal(bob) = %f, want 10", total)
	}

	total = mp.GetSpendingTotal("unknown")
	if total != 0 {
		t.Errorf("GetSpendingTotal(unknown) = %f, want 0", total)
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) should be 3")
	}
	if min(5, 3) != 3 {
		t.Error("min(5,3) should be 3")
	}
	if min(3, 3) != 3 {
		t.Error("min(3,3) should be 3")
	}
}
