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
	category := flag.String("category", "", "Category of presets to generate (e.g. Lead)")
	count := flag.Int("count", 1, "Number of presets to generate")
	single := flag.Bool("single", true, "Export in a single SysEx file")
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

	// load and parse category JSON
	jsonPath := fmt.Sprintf("categories/%s.json", *category)
	raw, err := fs.ReadFile(categoryFS, jsonPath)
	if err != nil {
		log.Fatalf("failed to read category JSON: %v", err)
	}
	var params map[string]ParamInfo
	if err := json.Unmarshal(raw, &params); err != nil {
		log.Fatalf("failed to parse category JSON: %v", err)
	}

	// validate category JSON against schema
	schemaLoader := gojsonschema.NewBytesLoader(schemaData)
	schema, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		log.Fatalf("failed to compile JSON schema: %v", err)
	}
	// extract schema properties
	var schemaStruct struct {
		Properties map[string]propSchema `json:"properties"`
	}
	if err := json.Unmarshal(schemaData, &schemaStruct); err != nil {
		log.Fatalf("failed to parse JSON schema: %v", err)
	}
	schemaProps := schemaStruct.Properties
	for name := range params {
		if _, exists := schemaProps[name]; !exists {
			log.Fatalf("category JSON contains unknown parameter '%s' not in schema", name)
		}
	}

	// build allowed values per parameter
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
		// ensure at least one value
		if maxVal < minVal {
			maxVal = minVal
		}
		countVals := maxVal - minVal + 1
		vals := make([]int, countVals)
		for i := 0; i < countVals; i++ {
			vals[i] = minVal + i
		}
		allowed[pname] = vals
	}

	// prepare output dir
	outDir := "presets"
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("failed to create presets directory: %v", err)
	}

	// generate unique patches with validation
	patches := make([][]byte, 0, *count)
	namesList := make([]string, 0, *count)
	seen := make(map[string]struct{})

	for len(patches) < *count {
		cfg := make(map[string]int, len(allowed))
		for pname, vals := range allowed {
			cfg[pname] = vals[rand.Intn(len(vals))]
		}
		// validate config
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
		name := strings.Title(strings.ToLower(rawName))
		namesList = append(namesList, name)
		patches = append(patches, buildPatch(name, catCode, params, cfg))
	}

	// output files
	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	first := namesList[0]
	var base string
	if *count > 1 {
		base = fmt.Sprintf("%s_%d_%s_%s", *category, *count, first, timeStr)
	} else {
		base = fmt.Sprintf("%s_%s_%s", *category, first, timeStr)
	}

	if *single {
		outPath := filepath.Join(outDir, base+".syx")
		if err := os.WriteFile(outPath, concat(patches), 0644); err != nil {
			log.Fatalf("failed to write %s: %v", outPath, err)
		}
		fmt.Printf("Wrote %d preset(s) to %s\n", *count, outPath)
	} else {
		for i, p := range patches {
			fname := fmt.Sprintf("%s_%02d_%s.syx", first, i+1, timeStr)
			if err := os.WriteFile(filepath.Join(outDir, fname), p, 0644); err != nil {
				log.Fatalf("failed to write %s: %v", fname, err)
			}
		}
		fmt.Printf("Wrote %d presets to %s directory\n", *count, outDir)
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
	// name
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
