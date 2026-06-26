package model

import "testing"

func TestTokenCalibratorColdStartUsesFirstValidRatio(t *testing.T) {
	c := NewTokenCalibrator()
	// 冷启动：首个落在硬区间内的观测直接作为初值。
	c.Observe("anthropic/opus", 100, 180)
	if got, want := c.Coefficient("anthropic/opus"), 1.8; got != want {
		t.Fatalf("Coefficient() = %v, want %v", got, want)
	}
}

func TestTokenCalibratorDefaultsToOne(t *testing.T) {
	c := NewTokenCalibrator()
	if got := c.Coefficient("unknown/model"); got != 1.0 {
		t.Fatalf("Coefficient(unknown) = %v, want 1.0", got)
	}
}

func TestTokenCalibratorRejectsOutOfRangeRatio(t *testing.T) {
	c := NewTokenCalibrator()
	// 比值落在硬区间 [0.25,4.0] 之外，视为不可信，不建立校准。
	c.Observe("p/m", 100, 10)   // ratio 0.1
	c.Observe("p/m", 100, 5000) // ratio 50
	if got := c.Coefficient("p/m"); got != 1.0 {
		t.Fatalf("Coefficient() = %v, want 1.0 (no calibration)", got)
	}
}

func TestTokenCalibratorRejectsRelativeOutlierAfterStable(t *testing.T) {
	c := NewTokenCalibrator()
	c.Observe("p/m", 100, 200) // 冷启动系数 2.0
	// 中转站抖动：真实 120k 却回传 60k，ratio≈0.6，落在 current*[0.5,2.0]=[1.0,4.0] 之外，应被忽略。
	c.Observe("p/m", 100000, 60000)
	if got, want := c.Coefficient("p/m"), 2.0; got != want {
		t.Fatalf("Coefficient() = %v, want %v (outlier ignored)", got, want)
	}
}

func TestTokenCalibratorEMASmoothsValidObservation(t *testing.T) {
	c := NewTokenCalibrator()
	c.Observe("p/m", 100, 200) // 系数 2.0
	// 合理范围内的新观测 ratio 2.4，EMA: 2.0*0.75 + 2.4*0.25 = 2.1。
	c.Observe("p/m", 100, 240)
	if got, want := c.Coefficient("p/m"), 2.1; got != want {
		t.Fatalf("Coefficient() = %v, want %v", got, want)
	}
}

func TestApplyCoefficientRoundsUp(t *testing.T) {
	if got, want := ApplyCoefficient(100, 1.5), 150; got != want {
		t.Fatalf("ApplyCoefficient(100,1.5) = %d, want %d", got, want)
	}
	if got, want := ApplyCoefficient(100, 1.0), 100; got != want {
		t.Fatalf("ApplyCoefficient at coef 1.0 = %d, want %d", got, want)
	}
	if got, want := ApplyCoefficient(3, 1.34), 5; got != want {
		t.Fatalf("ApplyCoefficient ceil = %d, want %d", got, want)
	}
}
