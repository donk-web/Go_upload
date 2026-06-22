package main

import (
	"archive/zip"
	"bufio"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var idCardPattern = regexp.MustCompile(`^(\d{15}|\d{17}[\dXx])$`)

var idCardHeaders = map[string]struct{}{
	"身份证号":   {},
	"身份证":    {},
	"身份证号码":  {},
	"证件号码":   {},
	"证件号":    {},
	"idcard": {},
}

func importBatchFile(dbPath string, insert func(sourceRow int, idCard string) error) (int, error) {
	switch strings.ToLower(filepath.Ext(dbPath)) {
	case ".csv":
		return importBatchCSV(dbPath, insert)
	case ".xlsx":
		return importBatchXLSX(dbPath, insert)
	case ".xls":
		return 0, errors.New("暂不支持旧版 .xls，请另存为 .xlsx 或 .csv 后重试")
	default:
		return 0, errors.New("仅支持 .xlsx 和 .csv 文件")
	}
}

func importBatchCSV(path string, insert func(sourceRow int, idCard string) error) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReaderSize(file, 128*1024))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	return importRows(func() ([]string, error) {
		row, err := reader.Read()
		if len(row) > 0 {
			row[0] = strings.TrimPrefix(row[0], "\uFEFF")
		}
		return row, err
	}, insert)
}

func importBatchXLSX(path string, insert func(sourceRow int, idCard string) error) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return 0, err
	}
	archive, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return 0, fmt.Errorf("Excel文件格式无效: %w", err)
	}

	sharedStrings, err := readSharedStrings(archive)
	if err != nil {
		return 0, err
	}
	worksheetPath := firstWorksheetPath(archive)
	sheet, err := openZipFile(archive, worksheetPath)
	if err != nil {
		return 0, errors.New("Excel中没有找到第一个工作表")
	}
	defer sheet.Close()

	decoder := xml.NewDecoder(sheet)
	rowNumber := 0
	headerColumn := -1
	total := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return total, fmt.Errorf("读取Excel工作表失败: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}

		var row xlsxRow
		if err := decoder.DecodeElement(&row, &start); err != nil {
			return total, fmt.Errorf("解析Excel第%d行失败: %w", rowNumber+1, err)
		}
		rowNumber++
		values := xlsxRowValues(row, sharedStrings)
		if headerColumn < 0 {
			headerColumn = findIDCardColumn(values)
			if headerColumn < 0 {
				if isBlankRow(values) {
					continue
				}
				return 0, errors.New("未找到身份证列，支持表头：身份证号、身份证、身份证号码、证件号码、证件号、id_card、idCard")
			}
			continue
		}
		if headerColumn >= len(values) {
			continue
		}
		if cellType := xlsxCellType(row, headerColumn); cellType == "" || cellType == "n" {
			if strings.TrimSpace(values[headerColumn]) != "" {
				return total, fmt.Errorf("Excel第%d行身份证为数字格式，请先将身份证列设置为文本格式后重新导入", rowNumber)
			}
		}
		idCard := normalizeIDCard(values[headerColumn])
		if idCard == "" {
			continue
		}
		if !idCardPattern.MatchString(idCard) {
			return total, fmt.Errorf("Excel第%d行身份证号码格式不正确: %s", rowNumber, maskIDCard(idCard))
		}
		if err := insert(rowNumber, idCard); err != nil {
			return total, err
		}
		total++
	}
	if headerColumn < 0 {
		return 0, errors.New("Excel中没有找到有效表头")
	}
	if total == 0 {
		return 0, errors.New("Excel中没有可导入的身份证号码")
	}
	return total, nil
}

func importRows(next func() ([]string, error), insert func(sourceRow int, idCard string) error) (int, error) {
	rowNumber := 0
	headerColumn := -1
	total := 0
	for {
		row, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return total, fmt.Errorf("读取CSV失败: %w", err)
		}
		rowNumber++
		if headerColumn < 0 {
			headerColumn = findIDCardColumn(row)
			if headerColumn < 0 {
				if isBlankRow(row) {
					continue
				}
				return 0, errors.New("未找到身份证列，支持表头：身份证号、身份证、身份证号码、证件号码、证件号、id_card、idCard")
			}
			continue
		}
		if headerColumn >= len(row) {
			continue
		}
		idCard := normalizeIDCard(row[headerColumn])
		if idCard == "" {
			continue
		}
		if !idCardPattern.MatchString(idCard) {
			return total, fmt.Errorf("CSV第%d行身份证号码格式不正确: %s", rowNumber, maskIDCard(idCard))
		}
		if err := insert(rowNumber, idCard); err != nil {
			return total, err
		}
		total++
	}
	if headerColumn < 0 {
		return 0, errors.New("CSV中没有找到有效表头")
	}
	if total == 0 {
		return 0, errors.New("CSV中没有可导入的身份证号码")
	}
	return total, nil
}

type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Reference string     `xml:"r,attr"`
	Type      string     `xml:"t,attr"`
	Value     string     `xml:"v"`
	Inline    xlsxInline `xml:"is"`
}

type xlsxInline struct {
	Text string     `xml:"t"`
	Runs []xlsxText `xml:"r"`
}

type xlsxText struct {
	Text string `xml:"t"`
}

type xlsxSharedStrings struct {
	Items []xlsxSharedItem `xml:"si"`
}

type xlsxSharedItem struct {
	Text string     `xml:"t"`
	Runs []xlsxText `xml:"r"`
}

func readSharedStrings(archive *zip.Reader) ([]string, error) {
	file, err := openZipFile(archive, "xl/sharedStrings.xml")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var data xlsxSharedStrings
	if err := xml.NewDecoder(file).Decode(&data); err != nil {
		return nil, fmt.Errorf("解析Excel共享文本失败: %w", err)
	}
	result := make([]string, 0, len(data.Items))
	for _, item := range data.Items {
		text := item.Text
		for _, run := range item.Runs {
			text += run.Text
		}
		result = append(result, text)
	}
	return result, nil
}

func openZipFile(archive *zip.Reader, name string) (io.ReadCloser, error) {
	for _, file := range archive.File {
		if file.Name == name {
			return file.Open()
		}
	}
	return nil, os.ErrNotExist
}

type xlsxWorkbook struct {
	Sheets []xlsxSheet `xml:"sheets>sheet"`
}

type xlsxSheet struct {
	RelationshipID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
}

type xlsxRelationships struct {
	Items []xlsxRelationship `xml:"Relationship"`
}

type xlsxRelationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
}

func firstWorksheetPath(archive *zip.Reader) string {
	const fallback = "xl/worksheets/sheet1.xml"
	workbookFile, err := openZipFile(archive, "xl/workbook.xml")
	if err != nil {
		return fallback
	}
	var workbook xlsxWorkbook
	decodeErr := xml.NewDecoder(workbookFile).Decode(&workbook)
	_ = workbookFile.Close()
	if decodeErr != nil || len(workbook.Sheets) == 0 || workbook.Sheets[0].RelationshipID == "" {
		return fallback
	}

	relsFile, err := openZipFile(archive, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return fallback
	}
	var relationships xlsxRelationships
	decodeErr = xml.NewDecoder(relsFile).Decode(&relationships)
	_ = relsFile.Close()
	if decodeErr != nil {
		return fallback
	}
	for _, relationship := range relationships.Items {
		if relationship.ID != workbook.Sheets[0].RelationshipID {
			continue
		}
		target := strings.TrimPrefix(strings.TrimSpace(relationship.Target), "/")
		if target == "" {
			return fallback
		}
		if strings.HasPrefix(target, "xl/") {
			return path.Clean(target)
		}
		return path.Clean(path.Join("xl", target))
	}
	return fallback
}

func xlsxRowValues(row xlsxRow, sharedStrings []string) []string {
	maxColumn := -1
	columns := make(map[int]string, len(row.Cells))
	for _, cell := range row.Cells {
		column := xlsxColumnIndex(cell.Reference)
		if column < 0 {
			continue
		}
		value := cell.Value
		switch cell.Type {
		case "s":
			index, err := strconv.Atoi(strings.TrimSpace(cell.Value))
			if err == nil && index >= 0 && index < len(sharedStrings) {
				value = sharedStrings[index]
			}
		case "inlineStr":
			value = cell.Inline.Text
			for _, run := range cell.Inline.Runs {
				value += run.Text
			}
		}
		columns[column] = value
		if column > maxColumn {
			maxColumn = column
		}
	}
	if maxColumn < 0 {
		return nil
	}
	values := make([]string, maxColumn+1)
	for column, value := range columns {
		values[column] = value
	}
	return values
}

func xlsxColumnIndex(reference string) int {
	index := 0
	found := false
	for _, char := range reference {
		if char < 'A' || char > 'Z' {
			break
		}
		index = index*26 + int(char-'A'+1)
		found = true
	}
	if !found {
		return -1
	}
	return index - 1
}

func xlsxCellType(row xlsxRow, targetColumn int) string {
	for _, cell := range row.Cells {
		if xlsxColumnIndex(cell.Reference) == targetColumn {
			return cell.Type
		}
	}
	return ""
}

func findIDCardColumn(row []string) int {
	for index, value := range row {
		if _, ok := idCardHeaders[normalizeHeader(value)]; ok {
			return index
		}
	}
	return -1
}

func normalizeHeader(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "\uFEFF"))
	value = strings.ToLower(value)
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", "\t", "", "\r", "", "\n", "", "：", "", ":", "")
	return replacer.Replace(value)
}

func normalizeIDCard(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `="`) && strings.HasSuffix(value, `"`) && len(value) > 3 {
		value = value[2 : len(value)-1]
	}
	value = strings.TrimPrefix(value, "'")
	value = strings.ReplaceAll(value, " ", "")
	return strings.ToUpper(value)
}

func isBlankRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func maskIDCard(value string) string {
	if len(value) < 10 {
		return value
	}
	return value[:6] + "********" + value[len(value)-4:]
}
