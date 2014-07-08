package gosu

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/MichaelTJones/walk"
	"github.com/mgutz/gosu/util"
)

const (
	// NotSlash is any rune but path separator.
	notSlash = "[^/]"
	// AnyRune is zero or more non-path separators.
	anyRune = notSlash + "*"
	// ZeroOrMoreDirectories is used by ** patterns.
	zeroOrMoreDirectories = "((?:[\\w\\.\\-]+\\/)*)"
	// TrailingStarStar matches everything inside directory.
	trailingStarStar = "/**"
	// SlashStarStarSlash maches zero or more directories.
	slashStarStarSlash = "/**/"
)

// RegexpInfo contains additional info about the Regexp created by a glob pattern.
type RegexpInfo struct {
	*regexp.Regexp
	Negate bool
}

// Globexp builds a regular express from from extended glob pattern and then
// returns a Regexp object from the pattern.
func Globexp(glob string) *regexp.Regexp {
	var re bytes.Buffer

	re.WriteString("^")

	i, inGroup := 0, false
	for i < len(glob) {
		r, w := utf8.DecodeRuneInString(glob[i:])

		switch r {
		default:
			re.WriteRune(r)

		case '\\', '$', '^', '+', '.', '(', ')', '=', '!', '|':
			re.WriteRune('\\')
			re.WriteRune(r)

		case '/':
			// TODO optimize later, string could be long
			rest := glob[i:]
			re.WriteRune('/')
			if strings.HasPrefix(rest, "/**/") {
				re.WriteString(zeroOrMoreDirectories)
				w *= 4
			} else if rest == "/**" {
				re.WriteString(".*")
				w *= 3
			}

		case '?':
			re.WriteRune('.')

		case '[', ']':
			re.WriteRune(r)

		case '{':
			inGroup = true
			re.WriteRune('(')

		case '}':
			inGroup = false
			re.WriteRune(')')

		case ',':
			if inGroup {
				re.WriteRune('|')
			} else {
				re.WriteRune('\\')
				re.WriteRune(r)
			}

		case '*':
			rest := glob[i:]
			if strings.HasPrefix(rest, "**/") {
				re.WriteString(zeroOrMoreDirectories)
				w *= 3
			} else {
				re.WriteString(anyRune)
			}
		}

		i += w
	}

	re.WriteString("$")
	//log.Printf("regex string %s", re.String())
	return regexp.MustCompile(re.String())
}

// Glob returns files and dirctories that match patterns.
//
// Special chars.
//
//   /**/   - match zero or more directories
//   {a,b}  - match a or b, no spaces
//   *      - match any non-separator char
//   ?      - match a single non-separator char
//   **/    - match any directory, start of pattern only
//   /**    - match any this directory, end of pattern only
//   !      - removes files from resultset, start of pattern only
//
func Glob(patterns []string) ([]*FileAsset, []*RegexpInfo, error) {
	// TODO very inefficient and unintelligent, optimize later

	m := map[string]*FileAsset{}
	regexps := []*RegexpInfo{}

	for _, pattern := range patterns {
		remove := strings.HasPrefix(pattern, "!")
		if remove {
			pattern = pattern[1:]
			re := Globexp(pattern)
			regexps = append(regexps, &RegexpInfo{Regexp: re, Negate: true})
			for path := range m {
				if re.MatchString(path) {
					m[path] = nil
				}
			}
		} else {
			re := Globexp(pattern)
			regexps = append(regexps, &RegexpInfo{Regexp: re})
			root := patternRoot(pattern)
			if root == "" {
				util.Panic("glob", "Cannot get root from pattern: %s", pattern)
			}
			fileAssets, err := walkFiles(root)
			if err != nil {
				return nil, nil, err
			}

			for _, file := range fileAssets {
				if re.MatchString(file.Path) {
					// TODO closure problem assigning &file
					tmp := file
					tmp.PatternRoot = root
					m[file.Path] = tmp
				}
			}
		}
	}

	//log.Printf("m %v", m)
	keys := []*FileAsset{}
	for _, it := range m {
		if it != nil {
			keys = append(keys, it)
		}
	}

	return keys, regexps, nil
}

// FileAsset contains file information and path from globbing.
type FileAsset struct {
	os.FileInfo
	// Path to asset
	Path string
	// PatternRoot is used to calculate offsets when writing to destination dir
	PatternRoot string
}

// hasMeta determines if a path has special chars used to build a Regexp.
func hasMeta(path string) bool {
	return strings.IndexAny(path, "*?[{") >= 0
}

// patternRoot gets a real directory root from a pattern. The directory
// returned is used as the start location for globbing.
func patternRoot(s string) string {
	// A negation does not walk the file system
	if strings.HasPrefix(s, "!") {
		return ""
	}
	// No directory in pattern
	parts := strings.Split(s, "/")
	if len(parts) == 1 {
		return "./"
	}
	// Build path until a dirname has a char used to build regex
	root, i, l := "", 0, len(parts)
	for i < l-1 {
		part := parts[i]
		if hasMeta(part) {
			break
		}
		if root == "" {
			root = part
		} else {
			root += "/" + part
		}
		i++
	}
	// Default to cwd
	if root == "" {
		root = "."
	}
	return root
}

// walkFiles walks a directory starting at root returning all directories and files
// include those found in subdirectories.
func walkFiles(root string) ([]*FileAsset, error) {
	fileAssets := []*FileAsset{}
	var lock sync.Mutex
	visitor := func(path string, info os.FileInfo, err error) error {
		if err == nil {
			lock.Lock()
			fileAssets = append(fileAssets, &FileAsset{FileInfo: info, Path: path})
			lock.Unlock()
		}
		return nil
	}
	err := walk.Walk(root, visitor)
	if err != nil {
		return nil, err
	}
	return fileAssets, nil
}
