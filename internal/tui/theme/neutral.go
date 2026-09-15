package theme

import (
	"image/color"
	"math"
)

// absoluteChroma 是颜色的绝对彩度（max-min，0..1）。
//
// 它比 HSL 饱和度更适合衡量「颜色有多鲜艳」：近白色（如 #e4e9f7）的
// HSL 饱和度高达 54%，但绝对彩度只有 0.075——视觉上只是「略带蓝调的白」。
func absoluteChroma(c color.RGBA) float64 {
	mx := math.Max(float64(c.R), math.Max(float64(c.G), float64(c.B))) / 255
	mn := math.Min(float64(c.R), math.Min(float64(c.G), float64(c.B))) / 255
	return mx - mn
}

// saturationForChroma 反解出「给定亮度下产生该绝对彩度」所需的 HSL 饱和度。
// HSL 的彩度 = (1-|2L-1|) * S，因此 S = chroma / (1-|2L-1|)。
func saturationForChroma(chroma, l float64) float64 {
	denom := 1 - math.Abs(2*l-1)
	if denom <= 1e-6 {
		return 0
	}
	return math.Min(1, chroma/denom)
}

// ensureContrastNeutral 适配中性色（正文、次要文字、装饰）。
//
// 与语义色不同，中性色要保留的是「绝对彩度」而不是 HSL 饱和度：
// 近白的浅蓝 #e4e9f7（HSL S=54%）压暗到中亮度时若保持 S，会变成刺眼的
// 高饱和蓝 #4e6fcb，让整个界面染上蓝调。保持绝对彩度则得到「带极淡蓝调的灰」，
// 既保留主题色温，又不会让中性文字显得脏。
func ensureContrastNeutral(fg, bg color.RGBA, minRatio float64) color.RGBA {
	if contrastRatio(fg, bg) >= minRatio {
		return fg
	}
	base := rgbToHSL(fg)
	chroma := absoluteChroma(fg)
	darken := !isDarkColor(bg)
	best := fg
	bestRatio := contrastRatio(fg, bg)
	for step := 0.02; step <= 1.0; step += 0.02 {
		cand := base
		if darken {
			cand.L = math.Max(0, base.L-step)
		} else {
			cand.L = math.Min(1, base.L+step)
		}
		cand.S = saturationForChroma(chroma, cand.L)
		rgba := hslToRGB(cand)
		if ratio := contrastRatio(rgba, bg); ratio > bestRatio {
			best, bestRatio = rgba, ratio
		}
		if bestRatio >= minRatio {
			break
		}
	}
	return best
}
