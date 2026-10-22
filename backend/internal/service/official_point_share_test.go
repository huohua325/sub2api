package service

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOfficialPointShareLab20261022(t *testing.T) {
	codexP := "11"
	grokP := "34"
	codexS := "22.48458835"
	grokS := "67.98602856"

	qq := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformOpenAI, U: "22.47711080", S: codexS, P: &codexP,
	}}, "50", nil)
	require.Equal(t, "10.9963", *qq.Points)
	require.Equal(t, 22, *qq.Percent)

	memslidesCodex := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformOpenAI, U: "0.00747755", S: codexS, P: &codexP,
	}}, "50", nil)
	require.Equal(t, "0.0037", *memslidesCodex.Points)
	require.Equal(t, 0, *memslidesCodex.Percent)

	memslidesGrok := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformGrok, U: "53.36744368", S: grokS, P: &grokP,
	}}, "80", nil)
	require.Equal(t, "26.6892", *memslidesGrok.Points)
	require.Equal(t, 33, *memslidesGrok.Percent)
	require.False(t, memslidesGrok.Blocked)

	huohua := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformGrok, U: "14.61805192", S: grokS, P: &grokP,
	}}, "80", nil)
	require.Equal(t, "7.3105", *huohua.Points)
	require.Equal(t, 9, *huohua.Percent)
	require.False(t, huohua.Blocked)

	penghao := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformGrok, U: "0.00053296", S: grokS, P: &grokP,
	}}, "80", nil)
	require.Equal(t, "0.0003", *penghao.Points)
	require.Equal(t, 0, *penghao.Percent)

	zero := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformGrok, U: "0", S: grokS, P: &grokP,
	}}, "80", nil)
	require.Equal(t, "0", *zero.Points)
	require.Equal(t, 0, *zero.Percent)

	missing := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformOpenAI, U: "1", S: "2", P: nil,
	}}, "50", nil)
	require.Nil(t, missing.Points)
	require.Nil(t, missing.Percent)

	emptyPool := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformOpenAI, U: "0", S: "0", P: &codexP,
	}}, "50", nil)
	require.Equal(t, "0", *emptyPool.Points)
	require.Equal(t, 0, *emptyPool.Percent)
	require.False(t, emptyPool.Blocked)
}

func TestOfficialPointShareDoesNotPoolSubscriptions(t *testing.T) {
	codexP := "11"
	separate := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformOpenAI, U: "22.47711080", S: "22.48458835", P: &codexP,
	}}, "50", nil)
	pooledU := new(big.Rat)
	pooledU.SetString("22.47711080")
	pooledU.Add(pooledU, pointMustRat("53.36744368"))
	pooledS := pointMustRat("22.48458835")
	pooledS.Add(pooledS, pointMustRat("67.98602856"))
	pooledP := pointMustRat("45")
	pooled := observedShare(pooledU, pooledS, pooledP)
	require.NotEqual(t, *pooled, *separate.Points)
}

func TestGrokWindowStartUsesPeriodWhenNotSevenDays(t *testing.T) {
	reset := time.Date(2026, 10, 27, 7, 32, 11, 64904000, time.FixedZone("CST", 8*3600))
	start := time.Date(2026, 10, 17, 7, 32, 11, 64904000, time.FixedZone("CST", 8*3600))
	got := officialWindowStart(PlatformGrok, reset, &start, &reset)
	require.True(t, got.Equal(start))
	fallback := officialWindowStart(PlatformGrok, reset, nil, nil)
	require.True(t, fallback.Equal(reset.Add(-7*24*time.Hour)))
	require.False(t, fallback.Equal(start))
}

func TestParseOfficialPointBudget(t *testing.T) {
	_, err := ParseOfficialPointBudget("50/1")
	require.Error(t, err)
	_, err = ParseOfficialPointBudget("1e2")
	require.Error(t, err)
	_, err = ParseOfficialPointBudget("0")
	require.Error(t, err)
	_, err = ParseOfficialPointBudget("1.23456")
	require.Error(t, err)
	got, err := ParseOfficialPointBudget(" 50.5 ")
	require.NoError(t, err)
	require.Equal(t, "101/2", got.RatString())
	normalized, err := NormalizeOfficialPointBudget(OfficialPointBudget{OpenAI: "50"})
	require.NoError(t, err)
	require.Equal(t, "80", normalized.Grok)
}

func TestOfficialPointParentsSumIntoOneCap(t *testing.T) {
	first := "20"
	second := "15"
	combined := OfficialPointsForPurpose([]OfficialPointInput{
		{Platform: PlatformGrok, U: "1", S: "1", P: &first},
		{Platform: PlatformGrok, U: "1", S: "1", P: &second},
	}, "30", nil)
	require.Equal(t, "35.0000", *combined.Points)
	require.True(t, combined.Blocked)
}

func TestOfficialPointCapRejectsAtThePointValue(t *testing.T) {
	full := "50"
	under := "49.9999"
	atCap := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformOpenAI, U: "1", S: "1", P: &full,
	}}, "50", nil)
	require.Equal(t, 100, *atCap.Percent)
	require.True(t, atCap.Blocked)

	almost := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformOpenAI, U: "1", S: "1", P: &under,
	}}, "50", nil)
	require.Equal(t, 100, *almost.Percent)
	require.False(t, almost.Blocked)

	missing := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformGrok, U: "1", S: "1", P: nil,
	}}, "80", nil)
	require.Nil(t, missing.Points)
	require.False(t, missing.Blocked)

	grokCap := "80"
	reached := OfficialPointsForPurpose([]OfficialPointInput{{
		Platform: PlatformGrok, U: "1", S: "1", P: &grokCap,
	}}, "80", nil)
	require.True(t, reached.Blocked)
}

func pointMustRat(text string) *big.Rat {
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		panic(text)
	}
	return value
}
