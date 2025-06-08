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
	"Bass":       0x01,
	"Lead":       0x02,
	"Pad":        0x03,
	"Keys":       0x04,
	"Organ":      0x05,
	"String":     0x06,
	"Brass":      0x07,
	"Percussion": 0x08,
	"Drone":      0x09,
	"Noise":      0x0A,
	"SFX":        0x0B,
	"Arp":        0x0C,
	"Misc":       0x0D,
	"User1":      0x0E,
	"User2":      0x0F,
	"User3":      0x10,
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Command-line flags
	category := flag.String("category", "", "Category of presets to generate (e.g. Lead, Pad)")
	count := flag.Int("count", 1, "Number of presets to generate")
	single := flag.Bool("single", true, "Export presets in a single SysEx file (default true)")
	output := flag.String("output", "generated.syx", "Output SysEx file name (or directory for single files)")
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

	// Generate unique presets
	patches := make([][]byte, 0, *count)
	names := make(map[string]struct{})
	configs := make(map[string]struct{})

	for len(patches) < *count {
		// Build parameter map
		config := make(map[string]int)
		for name, info := range params {
			val := rand.Intn(info.Max-info.Min+1) + info.Min
			config[name] = val
		}

		// Validate against schema
		result, err := schema.Validate(gojsonschema.NewGoLoader(config))
		if err != nil || !result.Valid() {
			continue
		}

		// Ensure distinct config
		cfgBytes, _ := json.Marshal(config)
		key := string(cfgBytes)
		if _, exists := configs[key]; exists {
			continue
		}
		configs[key] = struct{}{}

		// Generate unique name
		name := randomdata.Adjective()
		if len(name) > 8 {
			name = name[:8]
		}
		// Pad or re-roll on collision
		for {
			if _, used := names[name]; !used {
				names[name] = struct{}{}
				break
			}
			name = randomdata.Adjective()
			if len(name) > 8 {
				name = name[:8]
			}
		}

		// Build the SysEx patch
		patch := make([]byte, len(initPatch))
		copy(patch, initPatch)
		// Single preset identifier
		patch[5] = 0x02
		patch[6] = 0x03
		// Name: bytes 8-15
		nameBytes := []byte(name)
		for i := 0; i < 8; i++ {
			if i < len(nameBytes) {
				patch[8+i] = nameBytes[i]
			} else {
				patch[8+i] = 0x20 // space padding
			}
		}
		// Category byte at 16
		patch[16] = catCode

		// Apply parameters
		for pname, val := range config {
			offset := params[pname].SysexOffset
			patch[offset] = byte(val)
		}

		patches = append(patches, patch)
	}

	// Export
	if *single {
		// Concatenate and write to one file
		out := make([]byte, 0)
		for _, p := range patches {
			out = append(out, p...)
		}
		if err := os.WriteFile(*output, out, 0644); err != nil {
			log.Fatalf("failed to write output: %v", err)
		}
		fmt.Printf("Wrote %d presets to %s\n", len(patches), *output)
	} else {
		// Ensure output dir
		if err := os.MkdirAll(*output, 0755); err != nil {
			log.Fatalf("failed to create directory: %v", err)
		}
		for i, p := range patches {
			fpath := filepath.Join(*output, fmt.Sprintf("%s_%02d.syx", *category, i+1))
			if err := os.WriteFile(fpath, p, 0644); err != nil {
				log.Fatalf("failed to write %s: %v", fpath, err)
			}
		}
		fmt.Printf("Wrote %d presets to directory %s\n", len(patches), *output)
	}
}
