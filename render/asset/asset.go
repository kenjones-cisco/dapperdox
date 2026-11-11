// Package asset compiles deployment assets in precedence order into an immutable catalog.
package asset

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/viper"

	"github.com/kenjones-cisco/dapperdox/config"
	"github.com/kenjones-cisco/dapperdox/formatter"
)

// Catalog owns assets, metadata and markdown rules for one application lifetime.
// Construct it once before building documentation generations.
type Catalog struct {
	bindata       map[string][]byte
	metadata      map[string]map[string]string
	guideReplacer *strings.Replacer
	gfmReplace    []*gfmReplacer
}

var sectionSplitRegex = regexp.MustCompile(`\[\[[\w\-/]+\]\]`)

var (
	errMissingAsset    = errors.New("asset not found")
	errMissingSections = errors.New("no sections defined in overlay")
	errHTMLAsset       = errors.New("use .tmpl for HTML template fragments")
)

// NewCatalog reads all configured assets, including sections for APIs not yet discovered.
// Earlier directories win, matching the theme and overlay precedence.
func NewCatalog() (*Catalog, error) {
	c := &Catalog{bindata: make(map[string][]byte), metadata: make(map[string]map[string]string)}

	rewrite := viper.GetStringMapString(config.DocumentRewriteURL)

	const rewritePairSize = 2

	replacements := make([]string, 0, len(rewrite)*rewritePairSize)
	keys := make([]string, 0, len(rewrite))

	for key := range rewrite {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		replacements = append(replacements, key, rewrite[key])
	}

	c.guideReplacer = strings.NewReplacer(replacements...)
	if err := c.compileGFMMap(); err != nil {
		return nil, err
	}

	for _, src := range catalogSources() {
		if strings.HasSuffix(src.dir, string(filepath.Separator)+"sections") {
			if err := c.compileSections(src.dir); err != nil {
				return nil, err
			}

			continue
		}

		if err := c.compile(src.dir, src.prefix); err != nil {
			return nil, err
		}
	}

	return c, nil
}

type assetSource struct{ dir, prefix string }

const (
	templatePrefix = "assets/templates"
	assetPrefix    = "assets"
)

func catalogSources() []assetSource {
	custom, defaults := viper.GetString(config.AssetsDir), viper.GetString(config.DefaultAssetsDir)
	theme := viper.GetString(config.Theme)

	var sources []assetSource

	addSections := func(root string) {
		sources = append(sources, assetSource{filepath.Join(root, "sections"), templatePrefix})
	}

	if custom != "" {
		sources = append(sources,
			assetSource{filepath.Join(custom, "templates"), templatePrefix},
			assetSource{filepath.Join(custom, "static"), "assets/static"},
			assetSource{filepath.Join(custom, "themes", theme), assetPrefix})

		addSections(custom)
	}

	if theme != "" {
		dir := filepath.Join(defaults, "themes")
		if override := viper.GetString(config.ThemeDir); override != "" {
			dir = override
		}

		sources = append(sources, assetSource{filepath.Join(dir, theme), assetPrefix})
	}

	if theme != "default" {
		sources = append(sources, assetSource{filepath.Join(defaults, "themes", "default"), assetPrefix})
	}

	addSections(defaults)

	sources = append(sources,
		assetSource{filepath.Join(defaults, "templates"), templatePrefix},
		assetSource{filepath.Join(defaults, "static"), "assets/static"})

	return sources
}

// Asset returns a copy so callers cannot mutate the published catalog.
func (c *Catalog) Asset(name string) ([]byte, error) {
	if data, ok := c.bindata[strings.ReplaceAll(name, "\\", "/")]; ok {
		return bytes.Clone(data), nil
	}

	return nil, fmt.Errorf("%w: %s", errMissingAsset, name)
}

// Names returns asset names in stable order.
func (c *Catalog) Names() []string {
	names := make([]string, 0, len(c.bindata))
	for name := range c.bindata {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// RuleCount reports the bounded number of compiled markdown substitution rules.
func (c *Catalog) RuleCount() int { return len(c.gfmReplace) }

// MetaData returns a case-insensitive metadata field.
func (c *Catalog) MetaData(filename, name string) string {
	return c.metadata[strings.ReplaceAll(filename, "\\", "/")][strings.ToLower(name)]
}

func (c *Catalog) compileSections(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("scan sections %s: %w", dir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		for _, part := range []string{"templates", "static"} {
			prefix := filepath.Join(assetPrefix, part, entry.Name(), part)
			if err := c.compile(filepath.Join(dir, entry.Name(), part), prefix); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Catalog) compile(dir, prefix string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("asset path: %w", err)
	}

	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		return nil
	} else if statErr != nil {
		return statErr
	}

	err = filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}

			return nil
		}

		return c.compileFile(dir, path, prefix)
	})
	if err != nil {
		return fmt.Errorf("compile assets %s: %w", dir, err)
	}

	return nil
}

func (c *Catalog) compileFile(dir, path, prefix string) error {
	buf, readErr := os.ReadFile(path) //nolint:gosec // Paths come from walking operator-configured asset directories.
	if readErr != nil {
		return readErr
	}

	relative, relErr := filepath.Rel(dir, path)
	if relErr != nil {
		return relErr
	}

	ext := filepath.Ext(path)

	var meta map[string]string

	switch ext {
	case ".md":
		mdname := strings.TrimSuffix(relative, ext)

		buf, meta = processMetadata(buf)
		if strings.EqualFold(meta["overlay"], "true") {
			sections, headings := splitOnSection(string(buf))
			if sections == nil {
				return fmt.Errorf("%w %s", errMissingSections, path)
			}

			for i, heading := range headings {
				c.storeTemplate(prefix, filepath.Join(mdname, heading, "overlay.tmpl"), c.processMarkdown([]byte(sections[i])), meta)
			}
		} else {
			c.storeTemplate(prefix, mdname+".tmpl", c.processMarkdown(buf), meta)
		}
	case ".tmpl":
		buf, meta = processMetadata(buf)
		c.storeTemplate(prefix, relative, buf, meta)
	case ".html":
		return fmt.Errorf("refusing .html asset %s: %w", path, errHTMLAsset)
	default:
		c.storeTemplate(prefix, relative, buf, meta)
	}

	return nil
}

func (c *Catalog) storeTemplate(prefix, name string, data []byte, meta map[string]string) {
	name = filepath.ToSlash(filepath.Join(prefix, name))
	if _, exists := c.bindata[name]; exists {
		return
	}

	c.bindata[name] = []byte(c.guideReplacer.Replace(string(data)))
	if len(meta) != 0 {
		c.metadata[name] = meta
	}
}

func (c *Catalog) processMarkdown(doc []byte) []byte {
	html := formatter.Markdown(doc)
	for _, rep := range c.gfmReplace {
		html = rep.Regexp.ReplaceAll(html, rep.Replace)
	}

	return html
}

func processMetadata(doc []byte) (body []byte, meta map[string]string) {
	meta = make(map[string]string)

	lines := strings.Split(string(doc), "\n")
	for i, line := range lines {
		key, value, found := strings.Cut(line, ":")
		key = strings.TrimSpace(key)

		first, _ := utf8.DecodeRuneInString(key)
		if !found || key == "" || !unicode.IsLetter(first) {
			if line == "" {
				i++
			}

			body := strings.Join(lines[i:], "\n")
			if body != "" && !strings.HasSuffix(body, "\n") {
				body += "\n"
			}

			return []byte(body), meta
		}
		// Historically metadata ends at the next colon, including in URLs.
		value, _, _ = strings.Cut(value, ":")
		meta[strings.ToLower(key)] = strings.TrimSpace(value)
	}

	return nil, meta
}

func splitOnSection(text string) (sections, headings []string) {
	indexes := sectionSplitRegex.FindAllStringIndex(text, -1)
	if indexes == nil {
		return nil, nil
	}

	last := 0
	sections, headings = make([]string, len(indexes)), make([]string, len(indexes))

	for i, element := range indexes {
		if i > 0 {
			sections[i-1] = text[last:element[0]]
		}

		headings[i] = text[element[0]+2 : element[1]-2]
		last = element[1]
	}

	sections[len(indexes)-1] = text[last:]

	return sections, headings
}

func (c *Catalog) compileGFMMap() error {
	var candidates []string
	if dir := viper.GetString(config.AssetsDir); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "gfm.map"))
	}

	theme := viper.GetString(config.Theme)
	if dir := viper.GetString(config.ThemeDir); dir != "" {
		candidates = append(candidates, filepath.Join(dir, theme, "gfm.map"))
	}

	defaults := viper.GetString(config.DefaultAssetsDir)
	candidates = append(candidates, filepath.Join(defaults, "themes", theme, "gfm.map"), filepath.Join(defaults, "themes", "default", "gfm.map"))
	// Replace rather than append, even if a builder compiles its rules twice.
	var rules []*gfmReplacer

	for _, path := range candidates {
		file, err := os.Open(path) //nolint:gosec // Paths are selected from operator-configured markdown mapping directories.
		if os.IsNotExist(err) {
			continue
		}

		if err != nil {
			return fmt.Errorf("open markdown map: %w", err)
		}

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			pattern, replacement, found := strings.Cut(scanner.Text(), ":")
			if !found {
				continue
			}

			re, err := regexp.Compile(pattern)
			if err != nil {
				file.Close()

				return fmt.Errorf("markdown map %s: %w", path, err)
			}

			rules = append(rules, &gfmReplacer{Regexp: re, Replace: []byte(replacement)})
		}

		scanErr := scanner.Err()
		closeErr := file.Close()

		if scanErr != nil {
			return fmt.Errorf("markdown map %s: %w", path, scanErr)
		}

		if closeErr != nil {
			return closeErr
		}

		break
	}

	c.gfmReplace = rules

	return nil
}

type gfmReplacer struct {
	Regexp  *regexp.Regexp
	Replace []byte
}
