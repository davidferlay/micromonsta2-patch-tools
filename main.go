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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Pallinder/go-randomdata"
	"github.com/xeipuuv/gojsonschema"
)

//go:embed P292_Init.syx
var initPatch []byte

//go:embed specs/*.json
var specsFS embed.FS

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

// propSchema holds JSON schema constraints for a patch property
type propSchema struct {
	Minimum int `json:"minimum"`
	Maximum int `json:"maximum"`
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

	// flags
	specDir := flag.String("specs", "specs", "Directory containing category JSON spec files")
	category := flag.String("category", "", "Category of presets to generate (e.g. Lead)")
	count := flag.Int("count", 1, "Number of presets to generate")
	flag.Parse()

	// validate category
	if *category == "" {
		fmt.Println("Error: --category is required.")
		printAvailableCategories()
		os.Exit(1)
	}
	catCode, ok := categoryCodes[*category]
	if !ok {
		fmt.Printf("Error: unknown category '%s'.\n", *category)
		printAvailableCategories()
		os.Exit(1)
	}

	// load spec JSON
	jsonPath := fmt.Sprintf("%s/%s.json", *specDir, *category)
	var raw []byte
	var err error
	if *specDir == "specs" {
		raw, err = fs.ReadFile(specsFS, filepath.ToSlash(jsonPath))
	} else {
		raw, err = os.ReadFile(jsonPath)
	}
	if err != nil {
		log.Fatalf("failed to read spec JSON '%s': %v", jsonPath, err)
	}
	var params map[string]ParamInfo
	if err := json.Unmarshal(raw, &params); err != nil {
		log.Fatalf("failed to parse spec JSON: %v", err)
	}

	// compile JSON schema
	schemaLoader := gojsonschema.NewBytesLoader(schemaData)
	schema, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		log.Fatalf("failed to compile JSON schema: %v", err)
	}
	var schemaStruct struct {
		Properties map[string]propSchema `json:"properties"`
	}
	if err := json.Unmarshal(schemaData, &schemaStruct); err != nil {
		log.Fatalf("failed to parse JSON schema: %v", err)
	}
	schemaProps := schemaStruct.Properties

	// validate spec fields
	for name := range params {
		if _, exists := schemaProps[name]; !exists {
			log.Fatalf("spec JSON contains unknown parameter '%s' not in schema", name)
		}
	}

	// build allowed ranges
	allowed := make(map[string][]int, len(params))
	for pname, info := range params {
		minVal, maxVal := info.Min, info.Max
		ps := schemaProps[pname]
		if ps.Minimum > minVal {
			minVal = ps.Minimum
		}
		if ps.Maximum < maxVal {
			maxVal = ps.Maximum
		}
		if maxVal < minVal {
			maxVal = minVal
		}
		rng := maxVal - minVal + 1
		vals := make([]int, rng)
		for i := 0; i < rng; i++ {
			vals[i] = minVal + i
		}
		allowed[pname] = vals
	}

	// ensure output root
	rootDir := "presets"
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		log.Fatalf("failed to create presets directory: %v", err)
	}

	// generate patches
	patches := make([][]byte, 0, *count)
	namesList := make([]string, 0, *count)
	seen := make(map[string]struct{})

	for len(patches) < *count {
		cfg := make(map[string]int)
		for pname, vals := range allowed {
			cfg[pname] = vals[rand.Intn(len(vals))]
		}
		r, _ := schema.Validate(gojsonschema.NewGoLoader(cfg))
		if !r.Valid() {
			continue
		}
		key := configKey(cfg)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		rawName := uniqueName(seen)
		capName := strings.Title(strings.ToLower(rawName))
		namesList = append(namesList, capName)
		patches = append(patches, buildPatch(rawName, catCode, params, cfg))
	}

	// output logic
	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	if *count > 1 {
		// bundle
		bundleRaw := uniqueName(seen)
		bundleName := strings.Title(strings.ToLower(bundleRaw))
		subDir := filepath.Join(rootDir, bundleName)
		if err := os.MkdirAll(subDir, 0755); err != nil {
			log.Fatalf("failed to create bundle subdirectory: %v", err)
		}
		combined := fmt.Sprintf("%s_%s_bundle_%s.syx", *category, bundleName, timeStr)
		if err := os.WriteFile(filepath.Join(subDir, combined), concat(patches), 0644); err != nil {
			log.Fatalf("failed writing combined: %v", err)
		}
		fmt.Printf("Wrote combined %d presets to %s\n", *count, filepath.Join(subDir, combined))
		// individual
		for i, p := range patches {
			fname := fmt.Sprintf("%s_%s_%s.syx", *category, namesList[i], timeStr)
			if err := os.WriteFile(filepath.Join(subDir, fname), p, 0644); err != nil {
				log.Fatalf("failed writing %s: %v", fname, err)
			}
		}
		fmt.Printf("Wrote %d individual presets to %s\n", *count, subDir)
	} else {
		// single preset
		name := namesList[0]
		file := fmt.Sprintf("%s_%s_%s.syx", *category, name, timeStr)
		path := filepath.Join(rootDir, file)
		if err := os.WriteFile(path, concat(patches), 0644); err != nil {
			log.Fatalf("failed writing single: %v", err)
		}
		fmt.Printf("Wrote 1 preset to %s\n", path)
	}
}

func printAvailableCategories() {
	keys := make([]string, 0, len(categoryCodes))
	for k := range categoryCodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("Available categories:", strings.Join(keys, ", "))
}

func configKey(cfg map[string]int) string {
	b, _ := json.Marshal(cfg)
	return string(b)
}

func uniqueName(existing map[string]struct{}) string {
	for {
		// generate new raw name
		n := randomdata.Adjective()
		if len(n) > 8 {
			n = n[:8]
		}
		if _, ok := existing[n]; !ok {
			return n
		}
	}
}

func buildPatch(name string, catCode byte, params map[string]ParamInfo, cfg map[string]int) []byte {
	patch := make([]byte, len(initPatch))
	copy(patch, initPatch)
	// fixed header
	patch[0], patch[1], patch[2], patch[3], patch[4] = 0xF0, 0x00, 0x21, 0x22, 0x4D
	patch[5], patch[6], patch[7] = 0x02, 0x03, 0x09
	// name (lowercase)
	for i := 0; i < 8; i++ {
		if i < len(name) {
			patch[8+i] = name[i]
		} else {
			patch[8+i] = 0x20
		}
	}
	// category & reserved
	patch[16], patch[17], patch[18], patch[19] = catCode, 0x00, 0x00, 0x00
	// params
	for pname, val := range cfg {
		o := params[pname].SysexOffset
		patch[o] = byte(val)
	}
	// end
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
