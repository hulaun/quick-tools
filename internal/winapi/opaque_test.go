//go:build windows

package winapi

import "testing"

// The blend a plain edit paints a selection in here, and the one the palette
// wants instead: the system highlight and white, mapped onto a dark grey and
// white. The pixels are the ones a real capture of the search box gave back.
var (
	testFrom = [2][3]int32{channels(0x0078D4), channels(0xFFFFFF)}
	testTo   = [2][3]int32{channels(0x3A3A3A), channels(0xFFFFFF)}
)

func TestRemapRowLeavesRowsWithNoSelectionAlone(t *testing.T) {
	row := []uint32{0xff1C1C1C, 0xffE8E8E8, 0xff1C1C1C}
	want := append([]uint32(nil), row...)

	remapRow(row, testFrom, testTo)

	for i := range row {
		if row[i] != want[i] {
			t.Fatalf("pixel %d: got %06X, want %06X", i, row[i], want[i])
		}
	}
}

func TestRemapRowRewritesTheSelectionAndNothingElse(t *testing.T) {
	// Background, then a selection: its own colour, a glyph pixel drawn full
	// white over it, and a half-covered pixel at each end, then background
	// again. The two blends are what ClearType leaves at the edge of a letter --
	// one amount per channel, which is why each channel is undone on its own.
	row := []uint32{
		0xff1C1C1C, // background before the selection
		0xff0078D4, // the selection's own colour
		0xffFFFFFF, // a glyph, fully covered
		0xff0078D4,
		0xff3A78D4, // the last of it, half over the edge: matched by growing the run
		0xff1C1C1C, // background after
	}
	want := []uint32{
		0xff1C1C1C,
		0xff3A3A3A,
		0xffFFFFFF,
		0xff3A3A3A,
		0xff663A3A,
		0xff1C1C1C,
	}

	remapRow(row, testFrom, testTo)

	for i := range row {
		if row[i] != want[i] {
			t.Errorf("pixel %d: got %08X, want %08X", i, row[i], want[i])
		}
	}
}

func TestRemapRowLeavesAPixelThatIsNotOnTheBlend(t *testing.T) {
	// #FF872B is the highlight colour inverted -- a caret caught mid-blink, if
	// one is ever copied out despite being hidden first. It is nowhere on the
	// blend between the two ends, so there is no colour to map it to and it has
	// to survive untouched.
	row := []uint32{0xff0078D4, 0xffFF872B, 0xff0078D4}

	remapRow(row, testFrom, testTo)

	if row[1] != 0xffFF872B {
		t.Errorf("got %08X, want it left alone", row[1])
	}
	if row[0] != 0xff3A3A3A || row[2] != 0xff3A3A3A {
		t.Errorf("the selection around it should still be remapped: %08X %08X", row[0], row[2])
	}
}
