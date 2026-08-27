package ports

import (
	"fmt"
	"net"
	"testing"
)

func TestBasePort(t *testing.T) {
	tests := []struct {
		projectID    int
		expectedPort int
	}{
		{projectID: 0, expectedPort: 10000}, // Orbit Platform
		{projectID: 1, expectedPort: 10050}, // Orbit Core Services
		{projectID: 2, expectedPort: 10100}, // Client: Fryto
		{projectID: 3, expectedPort: 10150}, // Client: Kaazhe
		{projectID: 4, expectedPort: 10200}, // Client: Jtash
		{projectID: 5, expectedPort: 10250},
		{projectID: 10, expectedPort: 10500},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("ProjectID_%d", tt.projectID), func(t *testing.T) {
			got := BasePort(tt.projectID)
			if got != tt.expectedPort {
				t.Errorf("BasePort(%d) = %d, expected %d", tt.projectID, got, tt.expectedPort)
			}
			gotCalc := CalculateBasePort(tt.projectID)
			if gotCalc != tt.expectedPort {
				t.Errorf("CalculateBasePort(%d) = %d, expected %d", tt.projectID, gotCalc, tt.expectedPort)
			}
		})
	}
}

func TestDeterministicPort(t *testing.T) {
	// Valid slots 0..9 for project 0 (10000..10009)
	for slot := 0; slot <= 9; slot++ {
		port, err := DeterministicPort(0, slot)
		if err != nil {
			t.Fatalf("unexpected error for valid slot %d: %v", slot, err)
		}
		if port != 10000+slot {
			t.Errorf("expected port %d for slot %d, got %d", 10000+slot, slot, port)
		}
	}

	// Valid slots for project 2 (10100..10109)
	for slot := 0; slot <= 9; slot++ {
		port, err := DeterministicPort(2, slot)
		if err != nil {
			t.Fatalf("unexpected error for valid slot %d in project 2: %v", slot, err)
		}
		if port != 10100+slot {
			t.Errorf("expected port %d for slot %d, got %d", 10100+slot, slot, port)
		}
	}

	// Invalid slots (< 0 or > 9)
	invalidSlots := []int{-1, -5, 10, 11, 49, 50, 100}
	for _, slot := range invalidSlots {
		_, err := DeterministicPort(0, slot)
		if err == nil {
			t.Errorf("expected error for invalid deterministic slot %d, got nil", slot)
		}
	}

	// Invalid project ID (< 0)
	_, err := DeterministicPort(-1, 0)
	if err == nil {
		t.Errorf("expected error for negative project ID -1, got nil")
	}
}

func TestAllocateDynamic(t *testing.T) {
	projectID := 2 // Base port 10100, dynamic slots 10..49 (10110..10149)

	t.Run("Allocates lowest available dynamic port", func(t *testing.T) {
		port, err := AllocateDynamic(projectID, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// If port 10110 is free on system, it should be 10110
		if port < 10110 || port > 10149 {
			t.Fatalf("allocated port %d outside dynamic range 10110..10149", port)
		}
	})

	t.Run("Skips in-use ports in inUsePorts slice", func(t *testing.T) {
		inUse := []int{10110, 10111, 10112}
		port, err := AllocateDynamic(projectID, inUse)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port < 10113 || port > 10149 {
			t.Fatalf("expected allocated port >= 10113, got %d", port)
		}
	})

	t.Run("AllocateDynamicPort alias behaves identically", func(t *testing.T) {
		inUse := []int{10110, 10111}
		port, err := AllocateDynamicPort(projectID, inUse)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port < 10112 || port > 10149 {
			t.Fatalf("expected allocated port >= 10112, got %d", port)
		}
	})

	t.Run("Exhaustion when all 40 dynamic slots are in use", func(t *testing.T) {
		var allDynamicInUse []int
		for slot := 10; slot <= 49; slot++ {
			allDynamicInUse = append(allDynamicInUse, 10100+slot)
		}

		_, err := AllocateDynamic(projectID, allDynamicInUse)
		if err == nil {
			t.Fatal("expected exhaustion error when all dynamic slots are in use, got nil")
		}
	})

	t.Run("Returns error for negative project ID", func(t *testing.T) {
		_, err := AllocateDynamic(-1, nil)
		if err == nil {
			t.Fatal("expected error for negative project ID, got nil")
		}
	})
}

func TestIsPortAvailableAndInUse(t *testing.T) {
	// Invalid port numbers
	if IsPortAvailable(0) {
		t.Error("expected port 0 to not be available")
	}
	if IsPortAvailable(-1) {
		t.Error("expected port -1 to not be available")
	}
	if IsPortAvailable(70000) {
		t.Error("expected port 70000 to not be available")
	}
	if !IsPortInUse(0) {
		t.Error("expected port 0 to be reported as in use / unavailable")
	}

	// Find an ephemeral port to listen on
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open test listener: %v", err)
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("failed to cast address to *net.TCPAddr")
	}
	boundPort := tcpAddr.Port

	// While listener is active, port should NOT be available
	if IsPortAvailable(boundPort) {
		t.Errorf("port %d should not be available while listener is active", boundPort)
	}
	if !IsPortInUse(boundPort) {
		t.Errorf("port %d should be reported as in use while listener is active", boundPort)
	}

	// Dynamic allocator should skip active listener
	// Find which project block contains boundPort if any, or mock with a listener in project range
	testProjectID := 88 // base port: 10000 + 88*50 = 14400, dynamic: 14410..14449
	targetPort := 14410
	l2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", targetPort))
	if err == nil {
		defer l2.Close()
		// targetPort 14410 is active listener, so AllocateDynamic should skip 14410 and return 14411
		allocated, err := AllocateDynamic(testProjectID, nil)
		if err != nil {
			t.Fatalf("failed to allocate dynamic port: %v", err)
		}
		if allocated == targetPort {
			t.Errorf("AllocateDynamic allocated port %d which has an active socket listener", targetPort)
		}
		if allocated < 14411 || allocated > 14449 {
			t.Errorf("expected allocated port >= 14411, got %d", allocated)
		}
	}
}

func TestScanRangeAndProjectPorts(t *testing.T) {
	// Test ScanRange on empty or invalid inputs
	emptyMap := ScanRange(10050, 10040)
	if len(emptyMap) != 0 {
		t.Errorf("expected empty map for start > end, got %d elements", len(emptyMap))
	}

	emptyMap2 := ScanRange(-5, 10)
	if len(emptyMap2) != 0 {
		t.Errorf("expected empty map for negative startPort, got %d elements", len(emptyMap2))
	}

	emptyMap3 := ScanRange(10, 70000)
	if len(emptyMap3) != 0 {
		t.Errorf("expected empty map for endPort > 65535, got %d elements", len(emptyMap3))
	}

	// Open a test listener in a test range
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open listener: %v", err)
	}
	defer l.Close()

	port := l.Addr().(*net.TCPAddr).Port
	scanResult := ScanRange(port, port)
	if !scanResult[port] {
		t.Errorf("expected port %d to be reported as in use (true) in ScanRange", port)
	}

	// Test GetProjectRange
	start, end := GetProjectRange(3)
	if start != 10150 || end != 10199 {
		t.Errorf("GetProjectRange(3) = (%d, %d), expected (10150, 10199)", start, end)
	}

	// Test ScanProjectPorts
	res, err := ScanProjectPorts(3)
	if err != nil {
		t.Fatalf("ScanProjectPorts(3) returned unexpected error: %v", err)
	}
	if len(res) != 50 {
		t.Errorf("ScanProjectPorts(3) returned %d ports, expected 50", len(res))
	}

	// Negative project ID for ScanProjectPorts
	_, err = ScanProjectPorts(-1)
	if err == nil {
		t.Error("expected error for ScanProjectPorts(-1), got nil")
	}
}

func TestResolveProjectID(t *testing.T) {
	// Test default mappings
	tests := []struct {
		name        string
		expectedID  int
		expectedOK  bool
	}{
		{"orbit-platform", 0, true},
		{"orbit-services", 1, true},
		{"fryto", 2, true},
		{"kaazhe", 3, true},
		{"jtash", 4, true},
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

	// Test custom mapping
	custom := ProjectMapping{
		"my-custom-app": 42,
	}
	id, ok := ResolveProjectID("my-custom-app", custom)
	if !ok || id != 42 {
		t.Errorf("ResolveProjectID with custom mapping failed: id=%d, ok=%v", id, ok)
	}
}

func TestPortAllocationStruct(t *testing.T) {
	alloc := PortAllocation{
		Port:      10100,
		ProjectID: 2,
		Slot:      0,
		Service:   "frontend",
		Type:      Deterministic,
		InUse:     false,
	}

	if alloc.Port != 10100 || alloc.Type != Deterministic || alloc.InUse != false {
		t.Errorf("PortAllocation struct mismatch: %+v", alloc)
	}
}
