package domain

import (
	"fmt"
	"testing"
)

func TestBasePort(t *testing.T) {
	tests := []struct {
		projectID    int
		expectedPort int
	}{
		{projectID: 0, expectedPort: 10000},
		{projectID: 1, expectedPort: 10050},
		{projectID: 2, expectedPort: 10100},
		{projectID: 3, expectedPort: 10150},
		{projectID: 4, expectedPort: 10200},
		{projectID: 5, expectedPort: 10250},
		{projectID: 10, expectedPort: 10500},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("ProjectID_%d", tt.projectID), func(t *testing.T) {
			if got := BasePort(tt.projectID); got != tt.expectedPort {
				t.Errorf("BasePort(%d) = %d, expected %d", tt.projectID, got, tt.expectedPort)
			}
			if got := CalculateBasePort(tt.projectID); got != tt.expectedPort {
				t.Errorf("CalculateBasePort(%d) = %d, expected %d", tt.projectID, got, tt.expectedPort)
			}
		})
	}
}

func TestDeterministicPort(t *testing.T) {
	for slot := 0; slot <= 9; slot++ {
		port, err := DeterministicPort(0, slot)
		if err != nil {
			t.Fatalf("unexpected error for valid slot %d: %v", slot, err)
		}
		if port != 10000+slot {
			t.Errorf("expected port %d for slot %d, got %d", 10000+slot, slot, port)
		}
	}
	for slot := 0; slot <= 9; slot++ {
		port, err := DeterministicPort(2, slot)
		if err != nil {
			t.Fatalf("unexpected error for valid slot %d in project 2: %v", slot, err)
		}
		if port != 10100+slot {
			t.Errorf("expected port %d for slot %d, got %d", 10100+slot, slot, port)
		}
	}
	for _, slot := range []int{-1, -5, 10, 11, 49, 50, 100} {
		if _, err := DeterministicPort(0, slot); err == nil {
			t.Errorf("expected error for invalid deterministic slot %d, got nil", slot)
		}
	}
	if _, err := DeterministicPort(-1, 0); err == nil {
		t.Errorf("expected error for negative project ID -1, got nil")
	}
}

func TestAllocateDynamic(t *testing.T) {
	projectID := 2
	free := func(int) bool { return true }

	t.Run("Allocates lowest available dynamic port", func(t *testing.T) {
		port, err := AllocateDynamic(projectID, nil, free)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port != 10110 {
			t.Fatalf("expected lowest dynamic port 10110, got %d", port)
		}
	})

	t.Run("Skips in-use ports in inUse slice", func(t *testing.T) {
		port, err := AllocateDynamic(projectID, []int{10110, 10111, 10112}, free)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port != 10113 {
			t.Fatalf("expected 10113, got %d", port)
		}
	})

	t.Run("Skips ports the probe reports unavailable", func(t *testing.T) {
		port, err := AllocateDynamic(projectID, nil, func(p int) bool { return p != 10110 })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port != 10111 {
			t.Fatalf("expected 10111, got %d", port)
		}
	})

	t.Run("Exhaustion when all 40 dynamic slots are in use", func(t *testing.T) {
		var allDynamicInUse []int
		for slot := 10; slot <= 49; slot++ {
			allDynamicInUse = append(allDynamicInUse, 10100+slot)
		}
		if _, err := AllocateDynamic(projectID, allDynamicInUse, free); err == nil {
			t.Fatal("expected exhaustion error, got nil")
		}
	})

	t.Run("Returns error for negative project ID", func(t *testing.T) {
		if _, err := AllocateDynamic(-1, nil, free); err == nil {
			t.Fatal("expected error for negative project ID, got nil")
		}
	})
}

func TestGetProjectRange(t *testing.T) {
	start, end := GetProjectRange(3)
	if start != 10150 || end != 10199 {
		t.Errorf("GetProjectRange(3) = (%d, %d), expected (10150, 10199)", start, end)
	}
}

func TestResolveProjectID(t *testing.T) {
	tests := []struct {
		name       string
		expectedID int
		expectedOK bool
	}{
		{"orbit-platform", 0, true},
		{"manova", 1, true},
		{"orbit-services", 1, true},
		{"fryto", 2, true},
		{"jouybari", 3, true},
		{"jtash", 4, true},
		{"kohan_kherad", 5, true},
		{"unknown-project", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := ResolveProjectID(tt.name)
			if ok != tt.expectedOK {
				t.Errorf("ResolveProjectID(%q) ok = %v, expected %v", tt.name, ok, tt.expectedOK)
			}
			if ok && id != tt.expectedID {
				t.Errorf("ResolveProjectID(%q) id = %d, expected %d", tt.name, id, tt.expectedID)
			}
		})
	}

	custom := ProjectMapping{"my-custom-app": 42}
	id, ok := ResolveProjectID("my-custom-app", custom)
	if !ok || id != 42 {
		t.Errorf("ResolveProjectID with custom mapping failed: id=%d, ok=%v", id, ok)
	}
}

func TestPortAllocationStruct(t *testing.T) {
	alloc := PortAllocation{Port: 10100, ProjectID: 2, Slot: 0, Service: "frontend", Type: Deterministic, InUse: false}
	if alloc.Port != 10100 || alloc.Type != Deterministic || alloc.InUse != false {
		t.Errorf("PortAllocation struct mismatch: %+v", alloc)
	}
}
