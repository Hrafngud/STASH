package control

import (
	"math"
	"testing"
	"time"
)

func TestSmootherUsesExplicitDelta(t *testing.T) {
	t.Parallel()

	tau := 100 * time.Millisecond
	smoother, err := NewSmoother(tau, 0)
	if err != nil {
		t.Fatalf("NewSmoother returned error: %v", err)
	}

	unchanged, err := smoother.Step(1, 0)
	if err != nil {
		t.Fatalf("Step with zero delta returned error: %v", err)
	}
	if unchanged != 0 {
		t.Fatalf("Step(target=1, delta=0) = %v, want 0", unchanged)
	}

	afterTau, err := smoother.Step(1, tau)
	if err != nil {
		t.Fatalf("Step over one time constant returned error: %v", err)
	}
	assertClose(t, afterTau, 1-math.Exp(-1), 1e-15)
}

func TestSmootherIsIndependentOfDeltaPartitioning(t *testing.T) {
	t.Parallel()

	oneStep, _ := NewSmoother(time.Second, -2)
	twoSteps, _ := NewSmoother(time.Second, -2)
	oneResult, err := oneStep.Step(3, time.Second)
	if err != nil {
		t.Fatalf("one-step smoothing returned error: %v", err)
	}
	if _, err := twoSteps.Step(3, 400*time.Millisecond); err != nil {
		t.Fatalf("first partition returned error: %v", err)
	}
	twoResult, err := twoSteps.Step(3, 600*time.Millisecond)
	if err != nil {
		t.Fatalf("second partition returned error: %v", err)
	}
	assertClose(t, twoResult, oneResult, 1e-15)
}

func TestZeroDurationSmootherBypassesSmoothing(t *testing.T) {
	t.Parallel()

	smoother, err := NewSmoother(0, 10)
	if err != nil {
		t.Fatalf("NewSmoother returned error: %v", err)
	}
	got, err := smoother.Step(-5, 0)
	if err != nil {
		t.Fatalf("Step returned error: %v", err)
	}
	if got != -5 || smoother.Value() != -5 {
		t.Fatalf("zero-duration Step = %v, Value = %v; want -5", got, smoother.Value())
	}
}

func TestSmootherResetAndValidation(t *testing.T) {
	t.Parallel()

	if _, err := NewSmoother(-time.Millisecond, 0); err == nil {
		t.Error("NewSmoother accepted a negative time constant")
	}
	if _, err := NewSmoother(time.Second, math.NaN()); err == nil {
		t.Error("NewSmoother accepted a non-finite initial value")
	}

	smoother, _ := NewSmoother(time.Second, 0)
	if _, err := smoother.Step(math.Inf(1), time.Millisecond); err == nil {
		t.Error("Step accepted a non-finite target")
	}
	if _, err := smoother.Step(1, -time.Millisecond); err == nil {
		t.Error("Step accepted a negative delta")
	}
	if err := smoother.Reset(4); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}
	if smoother.Value() != 4 {
		t.Fatalf("Value after Reset = %v, want 4", smoother.Value())
	}
	if err := smoother.Reset(math.NaN()); err == nil {
		t.Error("Reset accepted NaN")
	}
	if smoother.Value() != 4 {
		t.Fatalf("failed Reset changed value to %v, want 4", smoother.Value())
	}
}
