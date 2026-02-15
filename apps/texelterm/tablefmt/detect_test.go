// Copyright © 2025 Texelation contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package tablefmt

import (
	"strings"
	"testing"
)

// ─── Markdown Detector ───────────────────────────────────────────────────────

func TestMarkdownDetector_Score_BasicTable(t *testing.T) {
	lines := []string{
		"| Name   | Age | City   |",
		"|--------|-----|--------|",
		"| Alice  | 30  | London |",
		"| Bob    | 25  | Paris  |",
	}
	d := &markdownDetector{}
	score := d.Score(lines)
	if score < 0.95 {
		t.Errorf("expected score >= 0.95 for valid markdown table, got %f", score)
	}
}

func TestMarkdownDetector_Score_NoSeparator(t *testing.T) {
	lines := []string{
		"| Name  | Age |",
		"| Alice | 30  |",
		"| Bob   | 25  |",
	}
	d := &markdownDetector{}
	score := d.Score(lines)
	if score > 0.0 {
		t.Errorf("expected score 0 without separator row, got %f", score)
	}
}

func TestMarkdownDetector_Score_NotATable(t *testing.T) {
	lines := []string{
		"This is just prose.",
		"No pipes here at all.",
		"Just plain text.",
	}
	d := &markdownDetector{}
	score := d.Score(lines)
	if score > 0.0 {
		t.Errorf("expected score 0 for prose, got %f", score)
	}
}

func TestMarkdownDetector_Score_NoLeadingTrailingPipes(t *testing.T) {
	lines := []string{
		"Name | Age | City",
		"-----|-----|-----",
		"Alice | 30 | London",
		"Bob | 25 | Paris",
	}
	d := &markdownDetector{}
	score := d.Score(lines)
	if score < 0.95 {
		t.Errorf("expected score >= 0.95 for markdown table without outer pipes, got %f", score)
	}
}

func TestMarkdownDetector_Compatible(t *testing.T) {
	d := &markdownDetector{}

	tests := []struct {
		line string
		want bool
	}{
		{"| Name | Age |", true},
		{"|------|-----|", true},
		{"", true},
		{"  ", true},
		{"just plain text", false},
	}
	for _, tt := range tests {
		got := d.Compatible(tt.line)
		if got != tt.want {
			t.Errorf("Compatible(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestMarkdownDetector_Parse_BasicTable(t *testing.T) {
	lines := []string{
		"| Name   | Age | City   |",
		"|--------|-----|--------|",
		"| Alice  | 30  | London |",
		"| Bob    | 25  | Paris  |",
	}
	d := &markdownDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if ts.tableType != tableMarkdown {
		t.Errorf("expected tableMarkdown, got %d", ts.tableType)
	}
	if ts.headerRow != 0 {
		t.Errorf("expected headerRow=0, got %d", ts.headerRow)
	}
	if len(ts.columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(ts.columns))
	}
	// Default alignment is left.
	for i, col := range ts.columns {
		if col.align != alignLeft {
			t.Errorf("column %d: expected alignLeft, got %d", i, col.align)
		}
	}
	// Separator row should NOT be in rows.
	if len(ts.rows) != 3 {
		t.Fatalf("expected 3 rows (header + 2 data), got %d", len(ts.rows))
	}
	if ts.rows[0][0] != "Name" {
		t.Errorf("expected header[0]='Name', got %q", ts.rows[0][0])
	}
	if ts.rows[1][1] != "30" {
		t.Errorf("expected data[0][1]='30', got %q", ts.rows[1][1])
	}
}

func TestMarkdownDetector_Parse_GFMAlignment(t *testing.T) {
	lines := []string{
		"| Left | Center | Right |",
		"|:-----|:------:|------:|",
		"| a    | b      | c     |",
	}
	d := &markdownDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if len(ts.columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(ts.columns))
	}
	wantAligns := []alignment{alignLeft, alignCenter, alignRight}
	for i, want := range wantAligns {
		if ts.columns[i].align != want {
			t.Errorf("column %d: expected alignment %d, got %d", i, want, ts.columns[i].align)
		}
	}
}

func TestMarkdownDetector_Parse_InconsistentColumnCount(t *testing.T) {
	lines := []string{
		"| A | B | C |",
		"|---|---|---|",
		"| 1 | 2 |",
		"| 3 | 4 | 5 |",
	}
	d := &markdownDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure (should handle missing cells)")
	}
	// Row with fewer columns should be padded with empty strings.
	if len(ts.rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(ts.rows))
	}
	if len(ts.rows[1]) != 3 {
		t.Errorf("expected 3 cells in row 1, got %d", len(ts.rows[1]))
	}
}

// ─── Pipe-Separated Detector ─────────────────────────────────────────────────

func TestPipeDetector_Score_BasicPipes(t *testing.T) {
	lines := []string{
		"Name | Age | City",
		"Alice | 30 | London",
		"Bob | 25 | Paris",
		"Carol | 28 | Tokyo",
	}
	d := &pipeDetector{}
	score := d.Score(lines)
	if score < 0.7 {
		t.Errorf("expected score >= 0.7 for consistent pipes, got %f", score)
	}
}

func TestPipeDetector_Score_RejectsMarkdownTable(t *testing.T) {
	lines := []string{
		"| Name | Age |",
		"|------|-----|",
		"| Alice | 30 |",
		"| Bob | 25 |",
	}
	d := &pipeDetector{}
	score := d.Score(lines)
	if score > 0.0 {
		t.Errorf("expected score 0 for markdown table (has separator), got %f", score)
	}
}

func TestPipeDetector_Score_InconsistentPipes(t *testing.T) {
	lines := []string{
		"Name | Age | City",
		"Alice | 30",
		"Bob | 25 | Paris | Extra",
		"Carol | 28 | Tokyo",
	}
	d := &pipeDetector{}
	score := d.Score(lines)
	if score >= 0.7 {
		t.Errorf("expected score < 0.7 for inconsistent pipe counts, got %f", score)
	}
}

func TestPipeDetector_Score_NoPipes(t *testing.T) {
	lines := []string{
		"This is prose.",
		"No pipes here.",
		"Just text.",
	}
	d := &pipeDetector{}
	score := d.Score(lines)
	if score > 0.0 {
		t.Errorf("expected score 0 for prose, got %f", score)
	}
}

func TestPipeDetector_Compatible(t *testing.T) {
	d := &pipeDetector{}

	tests := []struct {
		line string
		want bool
	}{
		{"Name | Age", true},
		{"", true},
		{"  ", true},
		{"no pipes here", false},
	}
	for _, tt := range tests {
		got := d.Compatible(tt.line)
		if got != tt.want {
			t.Errorf("Compatible(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestPipeDetector_Parse(t *testing.T) {
	lines := []string{
		"Name | Age | City",
		"Alice | 30 | London",
		"Bob | 25 | Paris",
	}
	d := &pipeDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if ts.tableType != tablePipeSeparated {
		t.Errorf("expected tablePipeSeparated, got %d", ts.tableType)
	}
	if ts.headerRow != 0 {
		t.Errorf("expected headerRow=0, got %d", ts.headerRow)
	}
	if len(ts.rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(ts.rows))
	}
	if ts.rows[0][0] != "Name" {
		t.Errorf("expected 'Name', got %q", ts.rows[0][0])
	}
	if ts.rows[1][2] != "London" {
		t.Errorf("expected 'London', got %q", ts.rows[1][2])
	}
}

// ─── Space-Aligned Detector ──────────────────────────────────────────────────

func TestSpaceAlignedDetector_Score_PsOutput(t *testing.T) {
	lines := []string{
		"  PID TTY          TIME CMD",
		"    1 ?        00:00:01 systemd",
		"    2 ?        00:00:00 kthreadd",
		"    3 ?        00:00:00 rcu_gp",
		"   10 ?        00:00:00 mm_percpu_wq",
	}
	d := &spaceAlignedDetector{}
	score := d.Score(lines)
	if score < 0.6 {
		t.Errorf("expected score >= 0.6 for ps output, got %f", score)
	}
}

func TestSpaceAlignedDetector_Score_KubectlOutput(t *testing.T) {
	lines := []string{
		"NAME                    READY   STATUS    RESTARTS   AGE",
		"nginx-7d8f8c8f-abc12   1/1     Running   0          5d",
		"redis-5c4d8c8f-xyz34   1/1     Running   0          3d",
		"app-6b3a7f9e-def56     1/1     Running   2          1d",
		"worker-9e8c7d6f-ghi78  0/1     Pending   0          2h",
	}
	d := &spaceAlignedDetector{}
	score := d.Score(lines)
	if score < 0.6 {
		t.Errorf("expected score >= 0.6 for kubectl output, got %f", score)
	}
}

func TestSpaceAlignedDetector_Score_Prose(t *testing.T) {
	lines := []string{
		"This is a paragraph of text that has no columns.",
		"It just flows naturally without any alignment.",
		"Nothing table-like about it whatsoever.",
	}
	d := &spaceAlignedDetector{}
	score := d.Score(lines)
	if score >= 0.6 {
		t.Errorf("expected score < 0.6 for prose, got %f", score)
	}
}

func TestSpaceAlignedDetector_Score_TooFewLines(t *testing.T) {
	lines := []string{
		"NAME   READY   STATUS",
		"nginx  1/1     Running",
	}
	d := &spaceAlignedDetector{}
	score := d.Score(lines)
	if score >= 0.6 {
		t.Errorf("expected low score for only 2 lines, got %f", score)
	}
}

func TestSpaceAlignedDetector_Compatible(t *testing.T) {
	d := &spaceAlignedDetector{}

	// Compatible is loose — most non-blank lines should pass.
	if !d.Compatible("") {
		t.Error("blank lines should be compatible")
	}
	if !d.Compatible("NAME   READY   STATUS") {
		t.Error("tabular line should be compatible")
	}
}

func TestSpaceAlignedDetector_Parse_KubectlOutput(t *testing.T) {
	lines := []string{
		"NAME                    READY   STATUS    RESTARTS   AGE",
		"nginx-7d8f8c8f-abc12   1/1     Running   0          5d",
		"redis-5c4d8c8f-xyz34   1/1     Running   0          3d",
		"app-6b3a7f9e-def56     1/1     Running   2          1d",
	}
	d := &spaceAlignedDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if ts.tableType != tableSpaceAligned {
		t.Errorf("expected tableSpaceAligned, got %d", ts.tableType)
	}
	if len(ts.columns) < 2 {
		t.Fatalf("expected at least 2 columns, got %d", len(ts.columns))
	}
	if ts.headerRow != 0 {
		t.Errorf("expected headerRow=0, got %d", ts.headerRow)
	}
	if len(ts.rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(ts.rows))
	}

	// Verify header values.
	headerVals := ts.rows[0]
	if headerVals[0] != "NAME" {
		t.Errorf("expected header[0]='NAME', got %q", headerVals[0])
	}

	// Verify a data value.
	if !strings.HasPrefix(ts.rows[1][0], "nginx") {
		t.Errorf("expected first data cell to start with 'nginx', got %q", ts.rows[1][0])
	}
}

func TestSpaceAlignedDetector_Parse_PsAux(t *testing.T) {
	lines := []string{
		"USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND",
		"root         1  0.0  0.1 169344 13092 ?        Ss   Feb01   0:03 /sbin/init",
		"root         2  0.0  0.0      0     0 ?        S    Feb01   0:00 [kthreadd]",
		"marc      1234  1.2  0.5 456789 12345 pts/0    Sl+  10:30   0:05 vim file.go",
	}
	d := &spaceAlignedDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure for ps aux output")
	}
	if len(ts.columns) < 2 {
		t.Fatalf("expected at least 2 columns, got %d", len(ts.columns))
	}
	if len(ts.rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(ts.rows))
	}
}

// ─── CSV/TSV Detector ────────────────────────────────────────────────────────

func TestCSVDetector_Score_CommaSeparated(t *testing.T) {
	lines := []string{
		"name,age,city",
		"Alice,30,London",
		"Bob,25,Paris",
		"Carol,28,Tokyo",
	}
	d := &csvDetector{}
	score := d.Score(lines)
	if score < 0.5 {
		t.Errorf("expected score >= 0.5 for CSV, got %f", score)
	}
}

func TestCSVDetector_Score_TabSeparated(t *testing.T) {
	lines := []string{
		"name\tage\tcity",
		"Alice\t30\tLondon",
		"Bob\t25\tParis",
		"Carol\t28\tTokyo",
	}
	d := &csvDetector{}
	score := d.Score(lines)
	if score < 0.5 {
		t.Errorf("expected score >= 0.5 for TSV, got %f", score)
	}
}

func TestCSVDetector_Score_Prose(t *testing.T) {
	lines := []string{
		"This is just text.",
		"No commas or tabs to speak of.",
		"Nothing CSV about it.",
	}
	d := &csvDetector{}
	score := d.Score(lines)
	if score > 0.0 {
		t.Errorf("expected score 0 for prose, got %f", score)
	}
}

func TestCSVDetector_Score_TooFewLines(t *testing.T) {
	lines := []string{
		"a,b,c",
		"1,2,3",
	}
	d := &csvDetector{}
	score := d.Score(lines)
	if score > 0.0 {
		t.Errorf("expected score 0 for fewer than 3 lines, got %f", score)
	}
}

func TestCSVDetector_Score_SingleColumn(t *testing.T) {
	lines := []string{
		"hello",
		"world",
		"test",
	}
	d := &csvDetector{}
	score := d.Score(lines)
	if score > 0.0 {
		t.Errorf("expected score 0 with no delimiters, got %f", score)
	}
}

func TestCSVDetector_Compatible(t *testing.T) {
	d := &csvDetector{}
	d.detectedDelim = ','
	d.detectedCount = 2

	tests := []struct {
		line string
		want bool
	}{
		{"a,b,c", true},     // count=2, matches exactly
		{"a,b", true},       // count=1, within +-1
		{"a,b,c,d", true},   // count=3, within +-1
		{"", true},          // blank
		{"no commas", false}, // count=0, not within +-1 of 2
	}
	for _, tt := range tests {
		got := d.Compatible(tt.line)
		if got != tt.want {
			t.Errorf("Compatible(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestCSVDetector_Parse_CSV(t *testing.T) {
	lines := []string{
		"name,age,city",
		"Alice,30,London",
		"Bob,25,Paris",
	}
	d := &csvDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if ts.tableType != tableCSV {
		t.Errorf("expected tableCSV, got %d", ts.tableType)
	}
	if ts.headerRow != 0 {
		t.Errorf("expected headerRow=0, got %d", ts.headerRow)
	}
	if len(ts.rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(ts.rows))
	}
	if ts.rows[0][0] != "name" {
		t.Errorf("expected 'name', got %q", ts.rows[0][0])
	}
	if ts.rows[1][2] != "London" {
		t.Errorf("expected 'London', got %q", ts.rows[1][2])
	}
}

func TestCSVDetector_Parse_TSV(t *testing.T) {
	lines := []string{
		"name\tage\tcity",
		"Alice\t30\tLondon",
		"Bob\t25\tParis",
	}
	d := &csvDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if ts.tableType != tableCSV {
		t.Errorf("expected tableCSV, got %d", ts.tableType)
	}
	if len(ts.rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(ts.rows))
	}
}

func TestCSVDetector_Parse_QuotedFields(t *testing.T) {
	lines := []string{
		`name,description,value`,
		`Alice,"Has a comma, here",100`,
		`Bob,"Simple",200`,
		`Carol,"Quoted ""word""",300`,
	}
	d := &csvDetector{}
	ts := d.Parse(lines)
	if ts == nil {
		t.Fatal("expected non-nil tableStructure")
	}
	if len(ts.rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(ts.rows))
	}
	if ts.rows[1][1] != "Has a comma, here" {
		t.Errorf("expected quoted field 'Has a comma, here', got %q", ts.rows[1][1])
	}
	if ts.rows[3][1] != `Quoted "word"` {
		t.Errorf("expected unescaped quotes, got %q", ts.rows[3][1])
	}
}

// ─── Detector priority ──────────────────────────────────────────────────────

func TestMarkdown_BeatsPipe(t *testing.T) {
	// A markdown table should score higher than the pipe detector.
	lines := []string{
		"| Name | Age |",
		"|------|-----|",
		"| Alice | 30 |",
		"| Bob | 25 |",
	}
	md := &markdownDetector{}
	pd := &pipeDetector{}

	mdScore := md.Score(lines)
	pdScore := pd.Score(lines)

	if mdScore <= pdScore {
		t.Errorf("markdown score (%f) should beat pipe score (%f)", mdScore, pdScore)
	}
}
