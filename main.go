package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"io/ioutil"
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

const patchSize = 176

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
	category := flag.String("category", "", "Category of presets to generate or replace (e.g. Lead)")
	count := flag.Int("count", 0, "Number of new presets to generate")
	editFile := flag.String("edit", "", "Existing SysEx file to edit")
	replace := flag.String("replace", "", "Comma-separated 1-based positions to replace")
	flag.Parse()

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
	raw, err := loadSpec(jsonPath, *specDir)
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

	// choose mode
	if *editFile != "" {
		runEdit(*editFile, *replace, catCode, params, allowed, schema)
	} else if *count > 0 {
		runGenerate(*count, *category, catCode, params, allowed, schema)
	} else {
		flag.Usage()
		os.Exit(1)
	}
}

func loadSpec(path, specDir string) ([]byte, error) {
	if specDir == "specs" {
		return fs.ReadFile(specsFS, filepath.ToSlash(path))
	}
	return os.ReadFile(path)
}

func runGenerate(count int, category string, catCode byte, params map[string]ParamInfo, allowed map[string][]int, schema *gojsonschema.Schema) {
	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	patches, names := generatePatches(count, catCode, params, allowed, schema)

	if count > 1 {
		bundleRaw := uniqueName(make(map[string]struct{}))
		bundleName := strings.Title(strings.ToLower(bundleRaw))
		subDir := filepath.Join("presets", bundleName)
		os.MkdirAll(subDir, 0755)
		combined := fmt.Sprintf("%s_%s_bundle_%s.syx", category, bundleName, timeStr)
		ioutil.WriteFile(filepath.Join(subDir, combined), concat(patches), 0644)
		fmt.Printf("Wrote combined %d presets to %s\n", count, filepath.Join(subDir, combined))
		for i, p := range patches {
			fname := fmt.Sprintf("%s_%s_%s.syx", category, names[i], timeStr)
			ioutil.WriteFile(filepath.Join(subDir, fname), p, 0644)
		}
		fmt.Printf("Wrote %d individual presets to %s\n", count, subDir)
	} else {
		path := filepath.Join("presets", fmt.Sprintf("%s_%s_%s.syx", category, names[0], timeStr))
		ioutil.WriteFile(path, concat(patches), 0644)
		fmt.Printf("Wrote 1 preset to %s\n", path)
	}
}

func runEdit(editFile, replaceList string, catCode byte, params map[string]ParamInfo, allowed map[string][]int, schema *gojsonschema.Schema) {
	data, err := os.ReadFile(editFile)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}
	n := len(data) / patchSize
	repl := parseReplaceList(replaceList, n)
	for _, idx := range repl {
		patches, _ := generatePatches(1, catCode, params, allowed, schema)
		off := idx * patchSize
		copy(data[off:off+patchSize], patches[0])
	}
	os.WriteFile(editFile, data, 0644)
	fmt.Printf("Replaced %d patches in %s\n", len(repl), editFile)
}

func parseReplaceList(list string, total int) []int {
	r := []int{}
	for _, token := range strings.Split(list, ",") {
		t := strings.TrimSpace(token)
		if num, err := strconv.Atoi(t); err == nil && num >= 1 && num <= total {
			r = append(r, num-1)
		}
	}
	return r
}

func generatePatches(count int, catCode byte, params map[string]ParamInfo, allowed map[string][]int, schema *gojsonschema.Schema) ([][]byte, []string) {
	patches := make([][]byte, 0, count)
	names := make([]string, 0, count)
	seen := make(map[string]struct{})
	for len(patches) < count {
		cfg := make(map[string]int)
		for pname, vals := range allowed {
			cfg[pname] = vals[rand.Intn(len(vals))]
		}
		r, _ := schema.Validate(gojsonschema.NewGoLoader(cfg))
		if !r.Valid() {
			continue
		}
		key := configKey(cfg)
		if _, ex := seen[key]; ex {
			continue
		}
		seen[key] = struct{}{}
		raw := uniqueName(seen)
		names = append(names, strings.Title(strings.ToLower(raw)))
		patches = append(patches, buildPatch(raw, catCode, params, cfg))
	}
	return patches, names
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
	patch[0], patch[1], patch[2], patch[3], patch[4] = 0xF0, 0x00, 0x21, 0x22, 0x4D
	patch[5], patch[6], patch[7] = 0x02, 0x03, 0x09
	for i := 0; i < 8; i++ {
		if i < len(name) {
			patch[8+i] = name[i]
		} else {
			patch[8+i] = 0x20
		}
	}
	patch[16] = catCode
	patch[17], patch[18], patch[19] = 0, 0, 0
	for pname, val := range cfg {
		o := params[pname].SysexOffset
		patch[o] = byte(val)
	}
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
