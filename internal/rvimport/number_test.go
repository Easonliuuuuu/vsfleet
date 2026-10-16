package rvimport

import (
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestParseNumberReadsFormattedCells(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"4", 4, true},
		{"9984", 9984, true},
		{"9,984", 9984, true},
		{"1,048,576", 1048576, true},
		{"-1,024", -1024, true},
		{"1,234.50", 1234.5, true},
		{"12.0", 12, true},
		{" 20,412 ", 20412, true},
		{"", 0, false},
		{"1,5", 0, false},
		{"12,34", 0, false},
		{"n/a", 0, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseNumber(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("parseNumber(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// RVTools stores capacity as number cells formatted "#,##0", which GetRows
// returns as "9,984". The values are those of the real RVTools 4.8.1.4
// export of the nested lab.
func TestImportReadsThousandsFormattedNumbers(t *testing.T) {
	f := openFixture(t, writeFixtureWorkbook(t, nil))
	style, err := f.NewStyle(&excelize.Style{NumFmt: 3}) // #,##0
	if err != nil {
		t.Fatal(err)
	}
	setNumber := func(sheet, header string, row int, value int) {
		t.Helper()
		rows, err := f.GetRows(sheet)
		if err != nil {
			t.Fatal(err)
		}
		for i, h := range rows[0] {
			if !strings.EqualFold(h, header) {
				continue
			}
			column, err := excelize.ColumnNumberToName(i + 1)
			if err != nil {
				t.Fatal(err)
			}
			cell := column + strconv.Itoa(row)
			if err := f.SetCellInt(sheet, cell, int64(value)); err != nil {
				t.Fatal(err)
			}
			if err := f.SetCellStyle(sheet, cell, cell, style); err != nil {
				t.Fatal(err)
			}
			return
		}
		t.Fatalf("%s has no %q column", sheet, header)
	}
	setNumber(sheetVCluster, "TotalCpu", 2, 9984)
	setNumber(sheetVCluster, "TotalMemory", 2, 20412)
	setNumber(sheetVInfo, "Memory", vInfoRow(t, f, "web-01"), 4096)

	result, err := Parse(f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cl := findCluster(result, "alpha", "cluster-a")
	if cl == nil || cl.TotalCPUMHz != 9984 || cl.TotalMemoryMB != 20412 {
		t.Errorf("cluster = %+v, want TotalCpu 9984 and TotalMemory 20412", cl)
	}
	if vm := findVM(result, "alpha", "web-01"); vm == nil || vm.MemoryMB != 4096 {
		t.Errorf("vm = %+v, want Memory 4096", vm)
	}
}
