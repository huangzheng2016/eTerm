package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestJumpChainPointsBackToHost(t *testing.T) {
	d, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AutoMigrate(&Host{}); err != nil {
		t.Fatal(err)
	}

	a := Host{Alias: "a", Hostname: "a", Port: 22, Username: "u", AuthMethod: "agent"}
	b := Host{Alias: "b", Hostname: "b", Port: 22, Username: "u", AuthMethod: "agent"}
	if err := d.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := d.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	jid := a.ID
	b.JumpHostID = &jid
	if err := d.Save(&b).Error; err != nil {
		t.Fatal(err)
	}

	// Editing A: jump to B would eventually reach A -> cycle
	if !JumpChainPointsBackToHost(d, a.ID, b.ID) {
		t.Fatal("expected cycle A<-B<-A")
	}
	// Editing B: jump to leaf A is ok
	if JumpChainPointsBackToHost(d, b.ID, a.ID) {
		t.Fatal("unexpected cycle for B -> A")
	}
}
