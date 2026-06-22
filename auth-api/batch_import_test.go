package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImportBatchCSVMatchesIDCardHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.csv")
	data := "\uFEFF姓名,身份证号码,备注\n张三,440101199001011234,测试\n李四,44010119900202123x,\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	var rows []batchImportRow
	total, err := importBatchCSV(path, func(sourceRow int, idCard string) error {
		rows = append(rows, batchImportRow{sourceRow: sourceRow, idCard: idCard})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	want := []batchImportRow{
		{sourceRow: 2, idCard: "440101199001011234"},
		{sourceRow: 3, idCard: "44010119900202123X"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
}

func TestImportBatchXLSXReadsSharedStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.xlsx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	writeZipText(t, archive, "xl/sharedStrings.xml", `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>身份证号</t></si>
  <si><t>440101199001011234</t></si>
  <si><t>44010119900202123X</t></si>
</sst>`)
	writeZipText(t, archive, "xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="B1" t="s"><v>0</v></c></row>
    <row r="2"><c r="B2" t="s"><v>1</v></c></row>
    <row r="3"><c r="B3" t="s"><v>2</v></c></row>
  </sheetData>
</worksheet>`)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	var idCards []string
	total, err := importBatchXLSX(path, func(_ int, idCard string) error {
		idCards = append(idCards, idCard)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	want := []string{"440101199001011234", "44010119900202123X"}
	if !reflect.DeepEqual(idCards, want) {
		t.Fatalf("idCards = %#v, want %#v", idCards, want)
	}
}

func TestNormalizeHeaderAliases(t *testing.T) {
	for _, header := range []string{"身份证号", " 身份证号码 ", "证件号码", "证件号", "id_card", "idCard"} {
		if column := findIDCardColumn([]string{"姓名", header}); column != 1 {
			t.Fatalf("header %q matched column %d, want 1", header, column)
		}
	}
}

func TestImportBatchXLSXRejectsNumericIDCard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numeric.xlsx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	writeZipText(t, archive, "xl/sharedStrings.xml", `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>身份证号</t></si></sst>`)
	writeZipText(t, archive, "xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c></row>
<row r="2"><c r="A2"><v>440101199001011234</v></c></row>
</sheetData></worksheet>`)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = importBatchXLSX(path, func(_ int, _ string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "文本格式") {
		t.Fatalf("err = %v, want text-format error", err)
	}
}

func TestImportBatchXLSXUsesFirstWorkbookSheet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "second-sheet.xlsx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	writeZipText(t, archive, "xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
 <sheets><sheet name="数据" sheetId="1" r:id="rId9"/></sheets>
</workbook>`)
	writeZipText(t, archive, "xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Id="rId9" Target="worksheets/sheet2.xml"/>
</Relationships>`)
	writeZipText(t, archive, "xl/sharedStrings.xml", `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
 <si><t>身份证号</t></si><si><t>440101199001011234</t></si>
</sst>`)
	writeZipText(t, archive, "xl/worksheets/sheet2.xml", `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
 <row r="1"><c r="A1" t="s"><v>0</v></c></row>
 <row r="2"><c r="A2" t="s"><v>1</v></c></row>
</sheetData></worksheet>`)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	total, err := importBatchXLSX(path, func(_ int, _ string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
}

func writeZipText(t *testing.T, archive *zip.Writer, name, content string) {
	t.Helper()
	writer, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}
