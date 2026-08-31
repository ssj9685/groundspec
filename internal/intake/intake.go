package intake

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const bundleVersion = 1

type Source struct {
	Path   string `json:"path"`
	Format string `json:"format"`
	Digest string `json:"digest"`
}

type Block struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Line    int      `json:"line"`
	Section []string `json:"section"`
	Text    string   `json:"text"`
}

type Bundle struct {
	Version int     `json:"version"`
	Source  Source  `json:"source"`
	Blocks  []Block `json:"blocks"`
}

func Ingest(projectRoot, sourcePath string) (Bundle, error) {
	root, absoluteSource, normalizedPath, err := resolveSource(projectRoot, sourcePath)
	if err != nil {
		return Bundle{}, err
	}
	_ = root
	format, err := detectFormat(sourcePath)
	if err != nil {
		return Bundle{}, err
	}
	raw, err := os.ReadFile(absoluteSource)
	if err != nil {
		return Bundle{}, err
	}
	if format != "pdf" && !utf8.Valid(raw) {
		return Bundle{}, fmt.Errorf("source must be valid UTF-8: %s", normalizedPath)
	}
	digest := sha256.Sum256(raw)
	var blocks []Block
	switch format {
	case "html":
		blocks, err = extractHTML(raw)
	case "markdown":
		blocks = extractMarkdown(string(raw))
	case "text":
		blocks = extractText(string(raw))
	case "pdf":
		blocks, err = extractPDF(absoluteSource)
	}
	if err != nil {
		return Bundle{}, err
	}
	assignBlockIDs(blocks)
	return Bundle{
		Version: bundleVersion,
		Source: Source{
			Path:   normalizedPath,
			Format: format,
			Digest: hex.EncodeToString(digest[:]),
		},
		Blocks: blocks,
	}, nil
}

func resolveSource(projectRoot, sourcePath string) (string, string, string, error) {
	if sourcePath == "" || filepath.IsAbs(sourcePath) {
		return "", "", "", errors.New("source path must be a non-empty project-relative path")
	}
	absoluteRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", "", "", err
	}
	root, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return "", "", "", fmt.Errorf("project root does not exist: %s", projectRoot)
	}
	joined := filepath.Join(root, filepath.FromSlash(sourcePath))
	relation, err := filepath.Rel(root, joined)
	if err != nil || relation == ".." || strings.HasPrefix(relation, ".."+string(filepath.Separator)) || filepath.IsAbs(relation) {
		return "", "", "", fmt.Errorf("source resolves outside the project root: %s", sourcePath)
	}
	resolved, err := filepath.EvalSymlinks(joined)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", "", fmt.Errorf("source does not exist: %s", sourcePath)
	}
	if err != nil {
		return "", "", "", err
	}
	resolvedRelation, err := filepath.Rel(root, resolved)
	if err != nil || resolvedRelation == ".." || strings.HasPrefix(resolvedRelation, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRelation) {
		return "", "", "", fmt.Errorf("source resolves outside the project root: %s", sourcePath)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", "", "", fmt.Errorf("source must be a regular file: %s", sourcePath)
	}
	return root, resolved, filepath.ToSlash(relation), nil
}

func detectFormat(sourcePath string) (string, error) {
	switch strings.ToLower(filepath.Ext(sourcePath)) {
	case ".html", ".htm":
		return "html", nil
	case ".md", ".markdown":
		return "markdown", nil
	case ".txt":
		return "text", nil
	case ".pdf":
		return "pdf", nil
	default:
		return "", fmt.Errorf("unsupported source format: %s", sourcePath)
	}
}

func extractPDF(sourcePath string) ([]Block, error) {
	executable, err := exec.LookPath("pdftotext")
	if err != nil {
		return nil, errors.New("PDF ingestion requires the external 'pdftotext' reader; install Poppler or configure a compatible reader")
	}
	command := exec.Command(executable, "-layout", "-enc", "UTF-8", "--", sourcePath, "-")
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pdftotext failed: %s", strings.TrimSpace(string(output)))
	}
	if !utf8.Valid(output) {
		return nil, errors.New("pdftotext returned invalid UTF-8")
	}
	blocks := extractText(string(output))
	if len(blocks) == 0 {
		return nil, errors.New("PDF contains no extractable text; use an external OCR reader before ingestion")
	}
	return blocks, nil
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func cloneSection(section []string) []string {
	if len(section) == 0 {
		return []string{}
	}
	return append([]string(nil), section...)
}

func updateSection(levels *[6]string, level int, title string) []string {
	if level < 1 {
		level = 1
	}
	if level > len(levels) {
		level = len(levels)
	}
	levels[level-1] = title
	for index := level; index < len(levels); index++ {
		levels[index] = ""
	}
	section := make([]string, 0, level)
	for index := 0; index < level; index++ {
		if levels[index] != "" {
			section = append(section, levels[index])
		}
	}
	return section
}

func currentSection(levels [6]string) []string {
	section := []string{}
	for _, title := range levels {
		if title != "" {
			section = append(section, title)
		}
	}
	return section
}

func assignBlockIDs(blocks []Block) {
	occurrences := map[string]int{}
	for index := range blocks {
		block := &blocks[index]
		fingerprint := block.Kind + "\x00" + strings.Join(block.Section, "\x00") + "\x00" + block.Text
		occurrence := occurrences[fingerprint]
		occurrences[fingerprint] = occurrence + 1
		digest := sha256.Sum256([]byte("groundspec/source-block/v1\x00" + fingerprint + "\x00" + strconv.Itoa(occurrence)))
		block.ID = "B-" + hex.EncodeToString(digest[:6])
	}
}

func extractText(source string) []Block {
	lines := strings.Split(source, "\n")
	blocks := []Block{}
	paragraph := []string{}
	paragraphLine := 0
	flush := func() {
		text := normalizeText(strings.Join(paragraph, " "))
		if text != "" {
			blocks = append(blocks, Block{Kind: "paragraph", Line: paragraphLine, Section: []string{}, Text: text})
		}
		paragraph = nil
		paragraphLine = 0
	}
	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if paragraphLine == 0 {
			paragraphLine = index + 1
		}
		paragraph = append(paragraph, line)
	}
	flush()
	return blocks
}

func extractMarkdown(source string) []Block {
	lines := strings.Split(source, "\n")
	blocks := []Block{}
	var levels [6]string
	paragraph := []string{}
	paragraphLine := 0
	inFence := false
	fenceMarker := ""
	fenceLine := 0
	codeLines := []string{}
	flushParagraph := func() {
		text := normalizeText(strings.Join(paragraph, " "))
		if text != "" {
			blocks = append(blocks, Block{Kind: "paragraph", Line: paragraphLine, Section: currentSection(levels), Text: text})
		}
		paragraph = nil
		paragraphLine = 0
	}
	flushCode := func() {
		text := normalizeText(strings.Join(codeLines, "\n"))
		if text != "" {
			blocks = append(blocks, Block{Kind: "code", Line: fenceLine, Section: currentSection(levels), Text: text})
		}
		codeLines = nil
		fenceLine = 0
	}
	for index, line := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(line)
		if inFence {
			if strings.HasPrefix(trimmed, fenceMarker) {
				flushCode()
				inFence = false
				fenceMarker = ""
			} else {
				codeLines = append(codeLines, line)
			}
			continue
		}
		if marker := markdownFence(trimmed); marker != "" {
			flushParagraph()
			inFence = true
			fenceMarker = marker
			fenceLine = lineNumber
			continue
		}
		if level, title, ok := markdownHeading(trimmed); ok {
			flushParagraph()
			section := updateSection(&levels, level, title)
			blocks = append(blocks, Block{Kind: "heading", Line: lineNumber, Section: section, Text: title})
			continue
		}
		if item, ok := markdownListItem(trimmed); ok {
			flushParagraph()
			blocks = append(blocks, Block{Kind: "list-item", Line: lineNumber, Section: currentSection(levels), Text: normalizeText(item)})
			continue
		}
		if trimmed == "" {
			flushParagraph()
			continue
		}
		if paragraphLine == 0 {
			paragraphLine = lineNumber
		}
		paragraph = append(paragraph, line)
	}
	if inFence {
		flushCode()
	}
	flushParagraph()
	return blocks
}

func markdownFence(trimmed string) string {
	if strings.HasPrefix(trimmed, "```") {
		return "```"
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return "~~~"
	}
	return ""
}

func markdownHeading(trimmed string) (int, string, bool) {
	level := 0
	for level < len(trimmed) && level < 6 && trimmed[level] == '#' {
		level++
	}
	if level == 0 || len(trimmed) <= level || !unicode.IsSpace(rune(trimmed[level])) {
		return 0, "", false
	}
	title := normalizeText(strings.TrimSpace(strings.TrimRight(trimmed[level:], "#")))
	return level, title, title != ""
}

func markdownListItem(trimmed string) (string, bool) {
	if len(trimmed) >= 2 && (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && unicode.IsSpace(rune(trimmed[1])) {
		return strings.TrimSpace(trimmed[2:]), true
	}
	index := 0
	for index < len(trimmed) && trimmed[index] >= '0' && trimmed[index] <= '9' {
		index++
	}
	if index > 0 && index+1 < len(trimmed) && trimmed[index] == '.' && unicode.IsSpace(rune(trimmed[index+1])) {
		return strings.TrimSpace(trimmed[index+2:]), true
	}
	return "", false
}

type htmlAccumulator struct {
	blocks       []Block
	levels       [6]string
	text         strings.Builder
	kind         string
	line         int
	headingLevel int
}

func (state *htmlAccumulator) start(kind string, line, headingLevel int) {
	state.flush()
	state.kind = kind
	state.line = line
	state.headingLevel = headingLevel
}

func (state *htmlAccumulator) appendText(value string, line int) {
	decoded := html.UnescapeString(value)
	if normalizeText(decoded) == "" {
		return
	}
	if state.kind == "" {
		state.kind = "paragraph"
		state.line = line
	}
	if state.text.Len() > 0 {
		state.text.WriteByte(' ')
	}
	state.text.WriteString(decoded)
}

func (state *htmlAccumulator) flush() {
	text := normalizeText(state.text.String())
	if text != "" {
		section := currentSection(state.levels)
		if state.kind == "heading" {
			section = updateSection(&state.levels, state.headingLevel, text)
		}
		state.blocks = append(state.blocks, Block{Kind: state.kind, Line: state.line, Section: cloneSection(section), Text: text})
	}
	state.text.Reset()
	state.kind = ""
	state.line = 0
	state.headingLevel = 0
}

func extractHTML(source []byte) ([]Block, error) {
	state := &htmlAccumulator{}
	line := 1
	index := 0
	skipTag := ""
	lowerSource := bytes.ToLower(source)
	for index < len(source) {
		if skipTag != "" {
			closing := []byte("</" + skipTag)
			relative := bytes.Index(lowerSource[index:], closing)
			if relative < 0 {
				break
			}
			end := index + relative
			line += bytes.Count(source[index:end], []byte("\n"))
			index = end
			skipTag = ""
			continue
		}
		if source[index] != '<' {
			next := bytes.IndexByte(source[index:], '<')
			if next < 0 {
				next = len(source) - index
			}
			chunk := source[index : index+next]
			state.appendText(string(chunk), line)
			line += bytes.Count(chunk, []byte("\n"))
			index += next
			continue
		}
		if bytes.HasPrefix(source[index:], []byte("<!--")) {
			end := bytes.Index(source[index+4:], []byte("-->"))
			if end < 0 {
				return nil, errors.New("unterminated HTML comment")
			}
			end += index + 7
			line += bytes.Count(source[index:end], []byte("\n"))
			index = end
			continue
		}
		tagEnd := htmlTagEnd(source, index)
		if tagEnd < 0 {
			return nil, errors.New("unterminated HTML tag")
		}
		tagLine := line
		tagSource := strings.TrimSpace(string(source[index+1 : tagEnd]))
		line += bytes.Count(source[index:tagEnd+1], []byte("\n"))
		index = tagEnd + 1
		if tagSource == "" || strings.HasPrefix(tagSource, "!") || strings.HasPrefix(tagSource, "?") {
			continue
		}
		closing := strings.HasPrefix(tagSource, "/")
		if closing {
			tagSource = strings.TrimSpace(tagSource[1:])
		}
		name := htmlTagName(tagSource)
		if name == "" {
			continue
		}
		if !closing && (name == "head" || name == "script" || name == "style" || name == "noscript" || name == "template") {
			state.flush()
			skipTag = name
			continue
		}
		if closing {
			if isHTMLBlock(name) {
				state.flush()
			}
			continue
		}
		if name == "br" {
			state.appendText(" ", tagLine)
			continue
		}
		if level := htmlHeadingLevel(name); level > 0 {
			state.start("heading", tagLine, level)
			continue
		}
		switch name {
		case "p", "div":
			state.start("paragraph", tagLine, 0)
		case "li":
			state.start("list-item", tagLine, 0)
		case "td", "th":
			state.start("table-cell", tagLine, 0)
		case "pre":
			state.start("code", tagLine, 0)
		}
	}
	state.flush()
	return state.blocks, nil
}

func htmlTagEnd(source []byte, start int) int {
	var quote byte
	for index := start + 1; index < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case '>':
			return index
		}
	}
	return -1
}

func htmlTagName(source string) string {
	end := 0
	for end < len(source) {
		character := source[end]
		if character == '/' || character == '>' || character == ' ' || character == '\t' || character == '\r' || character == '\n' {
			break
		}
		end++
	}
	return strings.ToLower(source[:end])
}

func htmlHeadingLevel(name string) int {
	if len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6' {
		return int(name[1] - '0')
	}
	return 0
}

func isHTMLBlock(name string) bool {
	if htmlHeadingLevel(name) > 0 {
		return true
	}
	switch name {
	case "p", "div", "li", "td", "th", "pre":
		return true
	default:
		return false
	}
}
