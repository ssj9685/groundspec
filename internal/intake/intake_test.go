package intake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestPB20PDFUsesExternalTextReader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-specific")
	}
	root := t.TempDir()
	tools := filepath.Join(root, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	reader := filepath.Join(tools, "pdftotext")
	if err := os.WriteFile(reader, []byte("#!/bin/sh\nprintf 'Assignment title\\n\\nRequired behavior\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(root, "brief.pdf"), []byte("%PDF-binary-fixture\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := Ingest(root, "brief.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Source.Format != "pdf" || len(bundle.Blocks) != 2 || bundle.Blocks[1].Text != "Required behavior" {
		t.Fatalf("bundle = %#v", bundle)
	}
}

func TestPB20ImageOnlyPDFRequiresExternalOCRReader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-specific")
	}
	root := t.TempDir()
	tools := filepath.Join(root, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	reader := filepath.Join(tools, "pdftotext")
	if err := os.WriteFile(reader, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(root, "scan.pdf"), []byte("%PDF-image-only-fixture\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Ingest(root, "scan.pdf")
	if err == nil || !strings.Contains(err.Error(), "external OCR reader") {
		t.Fatalf("image-only PDF error = %v", err)
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate intake test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "testdata", "intake"))
}

func TestTV12IngestIsDeterministicAndBindsRawSource(t *testing.T) {
	root := fixtureRoot(t)
	first, err := Ingest(root, "sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Ingest(root, "sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated ingestion differs:\nfirst=%+v\nsecond=%+v", first, second)
	}
	raw, err := os.ReadFile(filepath.Join(root, "sample.txt"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if first.Source.Digest != hex.EncodeToString(digest[:]) {
		t.Fatalf("source digest = %s", first.Source.Digest)
	}
	want := []Block{
		{Kind: "paragraph", Line: 1, Section: []string{}, Text: "First paragraph continues here."},
		{Kind: "paragraph", Line: 4, Section: []string{}, Text: "Second paragraph."},
	}
	assertBlocks(t, first.Blocks, want)
}

func TestTV13HTMLVisibleBlocksAndLocations(t *testing.T) {
	root := fixtureRoot(t)
	bundle, err := Ingest(root, "sample.html")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Source.Format != "html" || bundle.Source.Path != "sample.html" {
		t.Fatalf("source = %+v", bundle.Source)
	}
	want := []Block{
		{Kind: "heading", Line: 9, Section: []string{"Product & Scope"}, Text: "Product & Scope"},
		{Kind: "paragraph", Line: 10, Section: []string{"Product & Scope"}, Text: "First visible paragraph."},
		{Kind: "heading", Line: 12, Section: []string{"Product & Scope", "Requirements"}, Text: "Requirements"},
		{Kind: "list-item", Line: 14, Section: []string{"Product & Scope", "Requirements"}, Text: "Works offline."},
		{Kind: "list-item", Line: 15, Section: []string{"Product & Scope", "Requirements"}, Text: "Preserves source lines."},
		{Kind: "table-cell", Line: 17, Section: []string{"Product & Scope", "Requirements"}, Text: "Cell value"},
	}
	assertBlocks(t, bundle.Blocks, want)
	expectedBytes, err := os.ReadFile(filepath.Join(root, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected Bundle
	if err := json.Unmarshal(expectedBytes, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bundle, expected) {
		t.Fatalf("bundle does not match the frozen fixture\ngot=%+v\nwant=%+v", bundle, expected)
	}
}

func TestPB01HTMLAttributesCannotLeakIntoVisibleBlocks(t *testing.T) {
	root := t.TempDir()
	source := `<html>
<body>
<p title="comparison: 1 > 0">Visible &amp; safe.</p>
<script data-note=">">ignored</script>
</body>
</html>`
	if err := os.WriteFile(filepath.Join(root, "quoted.html"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle, err := Ingest(root, "quoted.html")
	if err != nil {
		t.Fatal(err)
	}
	want := []Block{{Kind: "paragraph", Line: 3, Section: []string{}, Text: "Visible & safe."}}
	assertBlocks(t, bundle.Blocks, want)
}

func TestPB02HTMLWithoutVisibleTextKeepsCanonicalEmptyBlockArray(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.html"), []byte("<!doctype html><html><head><title>Hidden</title></head><body><!-- no visible text --></body></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	bundle, err := Ingest(root, "empty.html")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Blocks == nil || len(bundle.Blocks) != 0 {
		t.Fatalf("blocks = %#v, want a non-nil empty array", bundle.Blocks)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"blocks":[]`) {
		t.Fatalf("canonical bundle encoded an invalid empty block collection: %s", encoded)
	}
}

func TestTV14MarkdownTypedBlocksAndSections(t *testing.T) {
	bundle, err := Ingest(fixtureRoot(t), "sample.md")
	if err != nil {
		t.Fatal(err)
	}
	want := []Block{
		{Kind: "heading", Line: 1, Section: []string{"Product"}, Text: "Product"},
		{Kind: "paragraph", Line: 3, Section: []string{"Product"}, Text: "Introductory paragraph continues on the next line."},
		{Kind: "heading", Line: 6, Section: []string{"Product", "Requirements"}, Text: "Requirements"},
		{Kind: "list-item", Line: 8, Section: []string{"Product", "Requirements"}, Text: "Works offline."},
		{Kind: "list-item", Line: 9, Section: []string{"Product", "Requirements"}, Text: "Preserves source lines."},
		{Kind: "code", Line: 11, Section: []string{"Product", "Requirements"}, Text: "fmt.Println(\"visible code\")"},
	}
	assertBlocks(t, bundle.Blocks, want)
}

func TestTV15RejectsOutsideAndUnsupportedSources(t *testing.T) {
	root := fixtureRoot(t)
	if _, err := Ingest(root, "../outside.txt"); err == nil {
		t.Fatal("source outside the project should be rejected")
	}
	unsupported := filepath.Join(t.TempDir(), "unsupported.docx")
	if err := os.WriteFile(unsupported, []byte("not a supported source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(filepath.Dir(unsupported), filepath.Base(unsupported)); err == nil || !strings.Contains(err.Error(), "unsupported source format") {
		t.Fatalf("unsupported format error = %v", err)
	}
}

func assertBlocks(t *testing.T, got, want []Block) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("block count = %d, want %d\nblocks=%+v", len(got), len(want), got)
	}
	for index := range want {
		withoutID := got[index]
		withoutID.ID = ""
		if !reflect.DeepEqual(withoutID, want[index]) {
			t.Fatalf("block %d = %+v, want %+v", index, withoutID, want[index])
		}
		if len(got[index].ID) != 14 || got[index].ID[:2] != "B-" {
			t.Fatalf("block %d has invalid id %q", index, got[index].ID)
		}
	}
}
