// Ports Tests/Helper/TableStyleTest.php and TableCellStyleTest.php
// (symfony/console). TableCellStyleTest's unknown-option case cannot occur
// with the typed TableCellStyleOptions; the invalid align check is tested
// instead.

package console

import (
	"errors"
	"testing"
)

func TestTableStyle_SetPadTypeWithInvalidType(t *testing.T) {
	err := NewTableStyle().SetPadType(31)
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindInvalidArgument || e.Message != "Invalid padding type. Expected one of (STR_PAD_LEFT, STR_PAD_RIGHT, STR_PAD_BOTH)." {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestTableStyle_SetPaddingCharEmpty(t *testing.T) {
	for _, c := range []string{"", "0"} {
		err := NewTableStyle().SetPaddingChar(c)
		var e *Error
		if !errors.As(err, &e) || e.Kind != KindLogic || e.Message != "The padding char must not be empty." {
			t.Fatalf("SetPaddingChar(%q): unexpected error %v", c, err)
		}
	}
}

func TestTableCellStyle_CreateTableCellStyle(t *testing.T) {
	style, err := NewTableCellStyle(TableCellStyleOptions{Fg: "red"})
	if err != nil {
		t.Fatal(err)
	}
	if got := style.Options().Fg; got != "red" {
		t.Errorf("fg = %q, want red", got)
	}

	_, err = NewTableCellStyle(TableCellStyleOptions{Align: "wrong"})
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindInvalidArgument || e.Message != "Wrong align value. Value must be following: 'left', 'center', 'right'." {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestTableCellStyle_TagOptions(t *testing.T) {
	style, err := NewTableCellStyle(TableCellStyleOptions{Fg: "red", Options: "bold,underscore ~"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := style.tagOptions(), "fg=red;bg=default;options=bold%2Cunderscore+%7E"; got != want {
		t.Errorf("tagOptions() = %q, want %q", got, want)
	}
}
