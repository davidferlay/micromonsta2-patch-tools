package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Pallinder/go-randomdata"
	"github.com/xeipuuv/gojsonschema"
)

//go:embed P292_Init.syx
var initPatch []byte

//go:embed categories/*.json
var categoryFS embed.FS

//go:embed micromonsta_patch_schema.json
var schemaData []byte

// ParamInfo holds metadata for a single synth parameter
type ParamInfo struct {
	Min         int    `json:"min"`
	Max         int    `json:"max"`
	Default     int    `json:"default"`
	SysexOffset int    `json:"sysex_offset"`
	SysexLength int    `json:"sysex_length"`
	Scale       string `json:"scale"`
	Unit        string `json:"unit"`
	Section     string `json:"section"`
}

// categoryCodes maps category names to SysEx category byte values
var categoryCodes = map[string]byte{
	"Bass":       0x00,
	"Lead":       0x01,
	"Pad":        0x02,
	"Keys":       0x03,
	"Organ":      0x04,
	"String":     0x05,
	"Brass":      0x06,
	"Percussion": 0x07,
	"Drone":      0x08,
	"Noise":      0x09,
	"SFX":        0x0A,
	"Arp":        0x0B,
	"Misc":       0x0C,
	"User1":      0x0D,
	"User2":      0x0E,
	"User3":      0x0F,
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Verify initPatch length
	if len(initPatch) != 176 {
		log.Fatalf("Init patch size is %d bytes; expected 176", len(initPatch))
	}

	// Command-line flags
	category := flag.String("category", "", "Category of presets to generate (e.g. Lead, Pad)")
	count := flag.Int("count", 1, "Number of presets to generate")
	single := flag.Bool("single", true, "Export presets in a single SysEx file (default true)")
	flag.Parse()

	if *category == "" {
		log.Fatalf("--category is required")
	}
	catCode, ok := categoryCodes[*category]
	if !ok {
		log.Fatalf("unknown category '%s'", *category)
	}

	// Load category JSON
	jsonPath := fmt.Sprintf("categories/%s.json", *category)
	data, err := fs.ReadFile(categoryFS, jsonPath)
	if err != nil {
		log.Fatalf("failed to read category JSON: %v", err)
	}
	var params map[string]ParamInfo
	if err := json.Unmarshal(data, &params); err != nil {
		log.Fatalf("failed to parse category JSON: %v", err)
	}

	// Load and compile JSON Schema
	schemaLoader := gojsonschema.NewBytesLoader(schemaData)
	schema, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		log.Fatalf("failed to compile JSON schema: %v", err)
	}

	// Ensure output directory
	outDir := "presets"
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("failed to create presets directory: %v", err)
	}

	// Generate unique presets
	var patches [][]byte
	var patchNames []string
	names := make(map[string]struct{})
	configs := make(map[string]struct{})

	for len(patches) < *count {
		// Randomize parameters
		config := make(map[string]int)
		for pname, info := range params {
			config[pname] = rand.Intn(info.Max-info.Min+1) + info.Min
		}
		// Validate and ensure distinct
		result, _ := schema.Validate(gojsonschema.NewGoLoader(config))
		if !result.Valid() {
			continue
		}
		key := string(mustJSON(config))
		if _, exists := configs[key]; exists {
			continue
		}
		configs[key] = struct{}{}

		// Unique, capitalized name
		rawName := uniqueName(names)
		name := strings.Title(strings.ToLower(rawName))
		names[name] = struct{}{}

		// Build patch
		patch := buildPatch(name, catCode, params, config)
		patches = append(patches, patch)
		patchNames = append(patchNames, name)
	}

	// Use Unix timestamp for file names
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	firstName := patchNames[0]

	if *single {
		// Single file name using category prefix and Unix timestamp
		n := *count
		var base string
		if n > 1 {
			base = fmt.Sprintf("%s_%d_%s_%s", *category, n, firstName, timestamp)
		} else {
			base = fmt.Sprintf("%s_%s_%s", *category, firstName, timestamp)
		}
		outPath := filepath.Join(outDir, base+".syx")
		if err := os.WriteFile(outPath, concat(patches), 0644); err != nil {
			log.Fatalf("failed to write %s: %v", outPath, err)
		}
		fmt.Printf("Wrote %d preset(s) to %s\n", *count, outPath)
	} else {
		// Multiple files
		for i, patch := range patches {
			fname := fmt.Sprintf("%s_%02d_%s.syx", firstName, i+1, timestamp)
			outPath := filepath.Join(outDir, fname)
			if err := os.WriteFile(outPath, patch, 0644); err != nil {
				log.Fatalf("failed to write %s: %v", outPath, err)
			}
		}
		fmt.Printf("Wrote %d presets to %s directory\n", *count, outDir)
	}
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func uniqueName(existing map[string]struct{}) string {
	for {
		name := randomdata.Adjective()
		if len(name) > 8 {
			name = name[:8]
		}
		if _, used := existing[name]; !used {
			return name
		}
	}
}

func buildPatch(name string, catCode byte, params map[string]ParamInfo, config map[string]int) []byte {
	patch := make([]byte, len(initPatch))
	copy(patch, initPatch)
	// Header & footer
	patch[0], patch[1], patch[2], patch[3], patch[4] = 0xF0, 0x00, 0x21, 0x22, 0x4D
	patch[5], patch[6], patch[7] = 0x02, 0x03, 0x09
	// Name
	for i := 0; i < 8; i++ {
		if i < len(name) {
			patch[8+i] = name[i]
		} else {
			patch[8+i] = 0x20
		}
	}
	// Category & zeros
	patch[16], patch[17], patch[18], patch[19] = catCode, 0x00, 0x00, 0x00
	// Params
	for pname, val := range config {
		offset := params[pname].SysexOffset
		patch[offset] = byte(val)
	}
	// End
	patch[len(patch)-1] = 0xF7
	return patch
}

func concat(patches [][]byte) []byte {
	var out []byte
	for _, p := range patches {
		out = append(out, p...)
	}
	return out
}
