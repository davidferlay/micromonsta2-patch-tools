package main

import (
	"bufio"
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

// PresetInfo holds information about a preset for sorting
type PresetInfo struct {
	Data     []byte
	Name     string
	Category string
	CatCode  byte
	Index    int // Original position for stable sorting
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

// getCategoryName returns the category name for a given byte code
func getCategoryName(catByte byte) string {
	for name, code := range categoryCodes {
		if code == catByte {
			return name
		}
	}
	return "Unknown"
}

// getCategoryOrder returns a sort order for categories
func getCategoryOrder(category string) int {
	order := map[string]int{
		"Bass":       0,
		"Lead":       1,
		"Pad":        2,
		"Keys":       3,
		"Organ":      4,
		"String":     5,
		"Brass":      6,
		"Percussion": 7,
		"Drone":      8,
		"Noise":      9,
		"SFX":        10,
		"Arp":        11,
		"Misc":       12,
		"User1":      13,
		"User2":      14,
		"User3":      15,
		"Unknown":    16,
	}
	if o, exists := order[category]; exists {
		return o
	}
	return 16
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// flags
	specDir := flag.String("specs", "specs", "Directory containing category JSON spec files")
	category := flag.String("category", "", "Category of presets to generate or replace (e.g. Lead)")
	count := flag.Int("count", 0, "Number of new presets to generate")
	editFile := flag.String("edit", "", "Existing SysEx file to edit")
	replace := flag.String("replace", "", "Comma-separated preset positions or names to replace")
	describeFile := flag.String("describe", "", "SysEx file to describe contents")
	splitFile := flag.String("split", "", "SysEx file to split into individual preset files")
	groupFiles := flag.String("group", "", "Comma-separated list of SysEx files to group into a single bundle")
	sortFile := flag.String("sort", "", "SysEx file to sort presets by category then alphabetically")
	flag.Parse()

	// describe mode
	if *describeFile != "" {
		runDescribe(*describeFile)
		return
	}

	// split mode
	if *splitFile != "" {
		runSplit(*splitFile)
		return
	}

	// group mode
	if *groupFiles != "" {
		runGroup(*groupFiles)
		return
	}

	// sort mode
	if *sortFile != "" {
		runSort(*sortFile)
		return
	}

	if *category == "" {
		fmt.Println("Error: --category is required for generate/edit operations.")
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
		sch := schemaProps[pname]
		if sch.Minimum > minVal {
			minVal = sch.Minimum
		}
		if sch.Maximum < maxVal {
			maxVal = sch.Maximum
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

func runSort(path string) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}

	n := len(data) / patchSize
	if n <= 1 {
		fmt.Printf("File %s contains only %d preset, nothing to sort.\n", path, n)
		return
	}

	fmt.Printf("Sorting %d presets in %s by category then alphabetically...\n", n, path)

	// Extract preset information
	presets := make([]PresetInfo, n)
	for i := 0; i < n; i++ {
		off := i * patchSize
		presetData := make([]byte, patchSize)
		copy(presetData, data[off:off+patchSize])

		name := strings.TrimRight(string(presetData[8:16]), " \x00")
		catByte := presetData[16]
		category := getCategoryName(catByte)

		presets[i] = PresetInfo{
			Data:     presetData,
			Name:     name,
			Category: category,
			CatCode:  catByte,
			Index:    i, // Original position for stable sorting
		}
	}

	// Show current order
	fmt.Println("Current order:")
	for i, preset := range presets {
		fmt.Printf("  %2d: %s (%s)\n", i+1, preset.Name, preset.Category)
	}

	// Sort presets: first by category order, then alphabetically by name, then by original index for stability
	sort.Slice(presets, func(i, j int) bool {
		catOrderI := getCategoryOrder(presets[i].Category)
		catOrderJ := getCategoryOrder(presets[j].Category)

		if catOrderI != catOrderJ {
			return catOrderI < catOrderJ
		}

		// Same category, sort alphabetically by name (case-insensitive)
		nameI := strings.ToLower(presets[i].Name)
		nameJ := strings.ToLower(presets[j].Name)
		if nameI != nameJ {
			return nameI < nameJ
		}

		// Same name, maintain stable sort using original index
		return presets[i].Index < presets[j].Index
	})

	// Show new order
	fmt.Println("\nNew order:")
	for i, preset := range presets {
		fmt.Printf("  %2d: %s (%s)\n", i+1, preset.Name, preset.Category)
	}

	// Rebuild the sysex data
	newData := make([]byte, len(data))
	for i, preset := range presets {
		off := i * patchSize
		copy(newData[off:off+patchSize], preset.Data)
	}

	// Create backup
	backupPath := strings.TrimSuffix(path, ".syx") + "_backup_" + strconv.FormatInt(time.Now().Unix(), 10) + ".syx"
	err = ioutil.WriteFile(backupPath, data, 0644)
	if err != nil {
		log.Printf("Warning: failed to create backup file: %v", err)
	} else {
		fmt.Printf("Created backup: %s\n", backupPath)
	}

	// Write sorted file
	err = ioutil.WriteFile(path, newData, 0644)
	if err != nil {
		log.Fatalf("failed to write sorted sysex file: %v", err)
	}

	fmt.Printf("Sorted presets written to %s\n", path)

	// Update descriptor file if it exists or if this is a multi-preset bundle
	if err := writeDescriptorFile(path, newData); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Summary of changes
	changes := 0
	for i, preset := range presets {
		if preset.Index != i {
			changes++
		}
	}
	fmt.Printf("Sorting complete: %d presets moved to new positions\n", changes)
}

func runGroup(fileList string) {
	// Parse file list
	filePaths := strings.Split(fileList, ",")
	var validFiles []string

	// Validate files and collect valid ones
	for _, path := range filePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		// Check if file exists and is readable
		if _, err := os.Stat(path); err != nil {
			fmt.Printf("Warning: skipping '%s' - %v\n", path, err)
			continue
		}

		// Check if file size is valid (multiple of patchSize)
		info, _ := os.Stat(path)
		if info.Size()%patchSize != 0 {
			fmt.Printf("Warning: skipping '%s' - invalid file size (%d bytes, not multiple of %d)\n",
				path, info.Size(), patchSize)
			continue
		}

		validFiles = append(validFiles, path)
	}

	if len(validFiles) == 0 {
		fmt.Println("Error: no valid sysex files found to group")
		return
	}

	fmt.Printf("Grouping %d sysex files into a single bundle:\n", len(validFiles))

	// Read and combine all presets
	var allPresets [][]byte
	var totalPresets int

	for _, path := range validFiles {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			fmt.Printf("Warning: failed to read '%s': %v\n", path, err)
			continue
		}

		numPresets := len(data) / patchSize
		fmt.Printf("  %s: %d preset(s)\n", filepath.Base(path), numPresets)

		// Extract individual presets from this file
		for i := 0; i < numPresets; i++ {
			off := i * patchSize
			preset := make([]byte, patchSize)
			copy(preset, data[off:off+patchSize])
			allPresets = append(allPresets, preset)
		}
		totalPresets += numPresets
	}

	if totalPresets == 0 {
		fmt.Println("Error: no presets found in any files")
		return
	}

	// Check for name conflicts and report them
	nameConflicts := findNameConflicts(allPresets)
	if len(nameConflicts) > 0 {
		fmt.Printf("Warning: found %d duplicate preset names:\n", len(nameConflicts))
		for name, count := range nameConflicts {
			fmt.Printf("  '%s' appears %d times\n", name, count)
		}
		fmt.Println("Proceeding anyway - duplicates will be preserved")
	}

	// Create output directory and files
	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	bundleRaw := uniqueName(make(map[string]struct{}))
	bundleName := strings.Title(strings.ToLower(bundleRaw))
	subDir := filepath.Join("presets", bundleName)
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		log.Fatalf("failed to create output directory: %v", err)
	}

	// Write combined bundle file
	combined := fmt.Sprintf("%s_grouped_%s.syx", bundleName, timeStr)
	combinedPath := filepath.Join(subDir, combined)
	combinedData := concat(allPresets)
	err = ioutil.WriteFile(combinedPath, combinedData, 0644)
	if err != nil {
		log.Fatalf("failed to write combined file: %v", err)
	}

	fmt.Printf("Wrote combined bundle with %d presets to %s\n", totalPresets, combinedPath)

	// Write individual preset files
	for _, preset := range allPresets {
		// Extract name and category
		name := strings.TrimRight(string(preset[8:16]), " \x00")
		catByte := preset[16]
		catName := getCategoryName(catByte)

		filename := fmt.Sprintf("%s_%s_%s.syx", catName, name, timeStr)
		filepath := filepath.Join(subDir, filename)

		err = ioutil.WriteFile(filepath, preset, 0644)
		if err != nil {
			log.Printf("Warning: failed to write individual preset %s: %v", filename, err)
		}
	}

	fmt.Printf("Wrote %d individual preset files to %s\n", totalPresets, subDir)

	// Write descriptor file
	if err := writeDescriptorFile(combinedPath, combinedData); err != nil {
		log.Printf("Warning: %v", err)
	}
}

func findNameConflicts(presets [][]byte) map[string]int {
	nameCounts := make(map[string]int)
	conflicts := make(map[string]int)

	for _, preset := range presets {
		name := strings.TrimRight(string(preset[8:16]), " \x00")
		nameCounts[name]++
		if nameCounts[name] > 1 {
			conflicts[name] = nameCounts[name]
		}
	}

	return conflicts
}

func runSplit(path string) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}

	n := len(data) / patchSize
	if n <= 1 {
		fmt.Printf("File %s contains only %d preset, nothing to split.\n", path, n)
		return
	}

	fmt.Printf("Splitting %d presets from %s into individual files:\n", n, path)

	// Create output directory based on input filename
	baseName := strings.TrimSuffix(filepath.Base(path), ".syx")
	outputDir := filepath.Join("presets", baseName+"_split")
	err = os.MkdirAll(outputDir, 0755)
	if err != nil {
		log.Fatalf("failed to create output directory: %v", err)
	}

	timeStr := strconv.FormatInt(time.Now().Unix(), 10)

	for i := 0; i < n; i++ {
		// Extract preset data
		off := i * patchSize
		presetData := data[off : off+patchSize]

		// Extract name and category
		name := strings.TrimRight(string(presetData[8:16]), " \x00")
		catByte := presetData[16]
		catName := getCategoryName(catByte)

		// Create filename: Category_PresetName_timestamp.syx
		filename := fmt.Sprintf("%s_%s_%s.syx", catName, name, timeStr)
		filepath := filepath.Join(outputDir, filename)

		// Write individual preset file
		err = ioutil.WriteFile(filepath, presetData, 0644)
		if err != nil {
			log.Printf("Warning: failed to write %s: %v", filepath, err)
			continue
		}

		fmt.Printf("  %2d: %s (%s) -> %s\n", i+1, name, catName, filename)
	}

	fmt.Printf("Split complete. %d individual preset files written to %s\n", n, outputDir)
}

func runDescribe(path string) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}
	n := len(data) / patchSize
	fmt.Printf("%d patches found in %s:\n", n, path)
	for i := 0; i < n; i++ {
		off := i * patchSize
		// extract name
		name := strings.TrimRight(string(data[off+8:off+16]), " \x00")
		// extract category
		catByte := data[off+16]
		catName := getCategoryName(catByte)
		fmt.Printf("%2d: %s (%s)\n", i+1, name, catName)
	}
}

func loadSpec(path, specDir string) ([]byte, error) {
	if specDir == "specs" {
		return fs.ReadFile(specsFS, filepath.ToSlash(path))
	}
	return os.ReadFile(path)
}

// runGenerate creates or updates bundle and writes a .txt descriptor
func runGenerate(count int, category string, catCode byte, params map[string]ParamInfo, allowed map[string][]int, schema *gojsonschema.Schema) {
	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	patches, names := generatePatches(count, catCode, params, allowed, schema)

	if count > 1 {
		// bundle directory
		bundleRaw := uniqueName(make(map[string]struct{}))
		bundleName := strings.Title(strings.ToLower(bundleRaw))
		subDir := filepath.Join("presets", bundleName)
		os.MkdirAll(subDir, 0755)
		// combined file (no category prefix for bundles)
		combined := fmt.Sprintf("%s_bundle_%s.syx", bundleName, timeStr)
		combinedPath := filepath.Join(subDir, combined)
		combinedData := concat(patches)
		ioutil.WriteFile(combinedPath, combinedData, 0644)
		fmt.Printf("Wrote combined %d presets to %s\n", count, combinedPath)

		// individual patches
		for i, p := range patches {
			fname := fmt.Sprintf("%s_%s_%s.syx", category, names[i], timeStr)
			ioutil.WriteFile(filepath.Join(subDir, fname), p, 0644)
		}
		fmt.Printf("Wrote %d individual presets to %s\n", count, subDir)

		// descriptor text file using unified function
		if err := writeDescriptorFile(combinedPath, combinedData); err != nil {
			log.Printf("Warning: %v", err)
		}
	} else {
		// single preset
		path := filepath.Join("presets", fmt.Sprintf("%s_%s_%s.syx", category, names[0], timeStr))
		ioutil.WriteFile(path, concat(patches), 0644)
		fmt.Printf("Wrote 1 preset to %s\n", path)
	}
}

// runEdit supports index- and name-based replacement
type replaceTarget struct {
	index int
	name  string
}

// extractExistingNames extracts all preset names from sysex data
func extractExistingNames(data []byte) []string {
	n := len(data) / patchSize
	names := make([]string, n)
	for i := 0; i < n; i++ {
		off := i*patchSize + 8
		names[i] = strings.TrimRight(string(data[off:off+8]), " \x00")
	}
	return names
}

// runEdit replaces patches and writes updated descriptor if multiple
func runEdit(editFile, replaceList string, catCode byte, params map[string]ParamInfo, allowed map[string][]int, schema *gojsonschema.Schema) {
	data, err := os.ReadFile(editFile)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}
	n := len(data) / patchSize

	// extract existing names
	existingNames := extractExistingNames(data)

	// parse replacement targets
	targets := parseReplaceList(replaceList, existingNames)

	// warn for unmatched tokens
	tokens := strings.Split(replaceList, ",")
	for _, tok := range tokens {
		t := strings.TrimSpace(tok)
		matched := false
		for _, tg := range targets {
			if tg.name != "" && strings.EqualFold(tg.name, t) {
				matched = true
			}
			if tg.index >= 0 {
				pos := strconv.Itoa(tg.index + 1)
				if pos == t {
					matched = true
				}
			}
		}
		if !matched {
			fmt.Printf("Warning: '%s' not found as preset name or position\n", t)
		}
	}

	// create exclusion set for name generation
	// include all existing names except those being replaced
	nameExclusions := make(map[string]struct{})
	replacedIndices := make(map[int]struct{})
	for _, tg := range targets {
		replacedIndices[tg.index] = struct{}{}
	}
	for i, name := range existingNames {
		if _, isReplaced := replacedIndices[i]; !isReplaced {
			nameExclusions[strings.ToLower(name)] = struct{}{}
		}
	}

	// apply replacements
	for _, tg := range targets {
		idx := tg.index
		if idx < 0 || idx >= n {
			fmt.Printf("Warning: position %d out of range\n", idx+1)
			continue
		}
		patches, names := generatePatchesWithExclusions(1, catCode, params, allowed, schema, nameExclusions)
		off := idx * patchSize
		copy(data[off:off+patchSize], patches[0])
		// Add the new name to exclusions to prevent future duplicates in this session
		nameExclusions[strings.ToLower(names[0])] = struct{}{}
	}

	err = os.WriteFile(editFile, data, 0644)
	if err != nil {
		log.Fatalf("failed to write sysex file: %v", err)
	}
	fmt.Printf("Replaced %d patches in %s\n", len(targets), editFile)

	// write descriptor using unified function
	if err := writeDescriptorFile(editFile, data); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Update output message for consistency
	if n > 1 {
		fmt.Printf("Updated descriptor file: %s\n", strings.TrimSuffix(editFile, ".syx")+".txt")
	}
}

// parseReplaceList handles names and positions
func parseReplaceList(list string, existingNames []string) []replaceTarget {
	t := []replaceTarget{}
	tokens := strings.Split(list, ",")
	for _, tok := range tokens {
		s := strings.TrimSpace(tok)
		// try position
		if num, err := strconv.Atoi(s); err == nil {
			if num >= 1 && num <= len(existingNames) {
				t = append(t, replaceTarget{index: num - 1, name: ""})
			} else {
				fmt.Printf("Warning: position %d out of range\n", num)
			}
			continue
		}
		// try name (case-insensitive)
		sLower := strings.ToLower(s)
		found := false
		for i, name := range existingNames {
			if strings.ToLower(name) == sLower {
				t = append(t, replaceTarget{index: i, name: name})
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("Warning: name '%s' not found among existing presets\n", s)
		}
	}
	return t
}

// writeDescriptorFile creates a descriptor text file for a sysex file
func writeDescriptorFile(sysexPath string, data []byte) error {
	n := len(data) / patchSize
	if n <= 1 {
		return nil // Don't create descriptor for single patches
	}

	descPath := strings.TrimSuffix(sysexPath, ".syx") + ".txt"
	f, err := os.Create(descPath)
	if err != nil {
		return fmt.Errorf("failed to create descriptor file: %v", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for i := 0; i < n; i++ {
		// Extract name from sysex data
		nameOff := i*patchSize + 8
		name := strings.TrimRight(string(data[nameOff:nameOff+8]), " \x00")

		// Extract category
		catByte := data[i*patchSize+16]
		catName := getCategoryName(catByte)
		fmt.Fprintf(w, "%2d: %s (%s)\n", i+1, name, catName)
	}
	w.Flush()
	fmt.Printf("Wrote descriptor to %s\n", descPath)
	return nil
}

// generatePatchesWithExclusions generates patches avoiding excluded names
func generatePatchesWithExclusions(count int, catCode byte, params map[string]ParamInfo, allowed map[string][]int, schema *gojsonschema.Schema, nameExclusions map[string]struct{}) ([][]byte, []string) {
	patches := make([][]byte, 0, count)
	names := make([]string, 0, count)
	seen := make(map[string]struct{})

	// Copy nameExclusions to seen to avoid collisions
	for name := range nameExclusions {
		seen[name] = struct{}{}
	}

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
		raw := uniqueNameWithExclusions(seen)
		names = append(names, raw)
		patches = append(patches, buildPatch(raw, catCode, params, cfg))
		// Add the new name to seen to prevent duplicates within this generation
		seen[strings.ToLower(raw)] = struct{}{}
	}
	return patches, names
}

// generatePatches now uses the new exclusion-aware function with empty exclusions
func generatePatches(count int, catCode byte, params map[string]ParamInfo, allowed map[string][]int, schema *gojsonschema.Schema) ([][]byte, []string) {
	return generatePatchesWithExclusions(count, catCode, params, allowed, schema, make(map[string]struct{}))
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

// uniqueNameWithExclusions generates unique names avoiding exclusions
func uniqueNameWithExclusions(existing map[string]struct{}) string {
	for {
		n := randomdata.Adjective()
		if len(n) > 8 {
			n = n[:8]
		}
		// Check both exact name and lowercase version for case-insensitive collision detection
		if _, ok := existing[n]; !ok {
			if _, ok := existing[strings.ToLower(n)]; !ok {
				return n
			}
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
