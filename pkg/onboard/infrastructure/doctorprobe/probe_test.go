package doctorprobe_test

import (
	"context"
	"testing"

	"github.com/manovaspace/orbit-cli/pkg/doctor"
	"github.com/manovaspace/orbit-cli/pkg/doctor/healer"
	"github.com/manovaspace/orbit-cli/pkg/onboard/infrastructure/doctorprobe"
)

func TestProbeUsesTheInjectedDiagnostics(t *testing.T) {
	report := &doctor.DoctorReport{Results: []doctor.DiagnosticResult{
		{Name: "Git", Status: doctor.StatusOK, Message: "ok"},
	}}
	probe := &doctorprobe.Probe{
		RunDiagnostics: func() *doctor.DoctorReport { return report },
		Registry:       healer.NewRegistry,
	}

	if got := probe.Run(); got != report {
		t.Errorf("Run returned %p, want the injected report %p", got, report)
	}
	if got := probe.HealableCount(report.Results); got != 0 {
		t.Errorf("HealableCount = %d, want 0 with an empty registry", got)
	}
	if err := probe.Heal(context.Background(), report.Results, nil); err != nil {
		t.Errorf("Heal failed: %v", err)
	}
}
