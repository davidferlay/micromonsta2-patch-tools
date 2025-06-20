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

	// Custom flag parsing to handle missing arguments better
	args := os.Args[1:]

	// Check for --change-category without value before flag parsing
	for i, arg := range args {
		if arg == "--change-category" || arg == "-change-category" {
			// Check if next argument exists and is not another flag
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				// Find the --edit file to provide context
				editFile := ""
				for j := 0; j < len(args)-1; j++ {
					if (args[j] == "--edit" || args[j] == "-edit") && j+1 < len(args) {
						editFile = args[j+1]
						break
					}
				}
				if editFile != "" {
					suggestCategoryFromFile(editFile)
				} else {
					fmt.Println("Error: --change-category requires a category value")
					printAvailableCategories()
				}
				os.Exit(1)
			}
		}
	}

	// flags
	specDir := flag.String("specs", "specs", "Directory containing category JSON spec files")
	category := flag.String("category", "", "Category of presets to generate or replace (e.g. Lead)")
	count := flag.Int("count", 0, "Number of new presets to generate")
	editFile := flag.String("edit", "", "Existing SysEx file to edit")
	replace := flag.String("replace", "", "Comma-separated preset positions or names to replace")
	replaceWith := flag.String("replace-with", "", "Comma-separated list of single preset .syx files to use as replacements")
	describeFile := flag.String("describe", "", "SysEx file to describe contents")
	splitFile := flag.String("split", "", "SysEx file to split into individual preset files")
	extractFrom := flag.String("extract", "", "Comma-separated list of preset positions (1-based) or names to extract from bundle")
	groupFiles := flag.String("group", "", "Comma-separated list of SysEx files or directories to group into a single bundle")
	sortFile := flag.String("sort", "", "SysEx file to sort presets by category then alphabetically")
	renameTo := flag.String("rename", "", "New name for the preset when editing single preset files (max 8 characters)")
	changeCategoryTo := flag.String("change-category", "", "New category for the preset when editing single preset files (e.g. Lead, Bass, Pad)")
	flag.Parse()

	// describe mode
	if *describeFile != "" {
		runDescribe(*describeFile)
		return
	}

	// split mode
	if *splitFile != "" {
		if *extractFrom != "" {
			// Extract specific presets from bundle
			runExtract(*splitFile, *extractFrom)
		} else {
			// Split all presets from bundle
			runSplit(*splitFile)
		}
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

	if *category == "" && *editFile != "" && *replaceWith == "" && *renameTo == "" && *changeCategoryTo == "" {
		fmt.Println("Error: --category is required for generate/edit operations (unless using --replace-with, --rename, or --change-category).")
		printAvailableCategories()
		os.Exit(1)
	}

	var catCode byte
	if *category != "" {
		var ok bool
		catCode, ok = categoryCodes[*category]
		if !ok {
			fmt.Printf("Error: unknown category '%s'.\n", *category)
			printAvailableCategories()
			os.Exit(1)
		}
	}

	// Validate change-category parameter if provided
	var changeCatCode byte
	if *changeCategoryTo != "" {
		var ok bool
		changeCatCode, ok = categoryCodes[*changeCategoryTo]
		if !ok {
			fmt.Printf("Error: unknown category '%s' for --change-category.\n", *changeCategoryTo)
			printAvailableCategories()
			os.Exit(1)
		}
	}

	var params map[string]ParamInfo
	var allowed map[string][]int
	var schema *gojsonschema.Schema

	// Only load specs and schema if we need them (for random generation)
	if *category != "" {
		// load spec JSON
		jsonPath := fmt.Sprintf("%s/%s.json", *specDir, *category)
		raw, err := loadSpec(jsonPath, *specDir)
		if err != nil {
			log.Fatalf("failed to read spec JSON '%s': %v", jsonPath, err)
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			log.Fatalf("failed to parse spec JSON: %v", err)
		}

		// compile JSON schema
		schemaLoader := gojsonschema.NewBytesLoader(schemaData)
		schema, err = gojsonschema.NewSchema(schemaLoader)
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
		allowed = make(map[string][]int, len(params))
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
	}

	// choose mode
	if *editFile != "" {
		if *renameTo != "" && *changeCategoryTo != "" {
			// Combined rename and category change mode
			runRenameAndChangeCategory(*editFile, *renameTo, changeCatCode)
		} else if *renameTo != "" {
			// Rename mode for single preset files
			runRename(*editFile, *renameTo)
		} else if *changeCategoryTo != "" {
			// Category change mode for single preset files
			runChangeCategory(*editFile, changeCatCode)
		} else if *category != "" {
			// Random generation replacement mode
			runEdit(*editFile, *replace, catCode, params, allowed, schema)
		} else if *replaceWith != "" {
			// File-based replacement mode
			runEditWithFiles(*editFile, *replace, *replaceWith)
		} else {
			fmt.Println("Error: --edit requires one of: --rename (for renaming), --change-category (for category change), --category (for random generation), or --replace-with (for file replacement)")
			os.Exit(1)
		}
	} else if *count > 0 {
		if *category == "" {
			fmt.Println("Error: --count requires --category")
			os.Exit(1)
		}
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

func runRename(filePath, newName string) {
	// Validate new name length
	if len(newName) > 8 {
		fmt.Printf("Warning: new name '%s' is longer than 8 characters, truncating to '%s'\n", newName, newName[:8])
		newName = newName[:8]
	}

	// Check if file exists
	if _, err := os.Stat(filePath); err != nil {
		log.Fatalf("failed to access file '%s': %v", filePath, err)
	}

	// Read the file
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}

	// Check if it's a single preset
	if len(data) != patchSize {
		log.Fatalf("file '%s' is not a single preset file (size: %d bytes, expected: %d)", filePath, len(data), patchSize)
	}

	// Extract current preset info
	currentName := strings.TrimRight(string(data[8:16]), " \x00")
	catByte := data[16]
	category := getCategoryName(catByte)

	fmt.Printf("Renaming preset '%s' (%s) to '%s'\n", currentName, category, newName)

	// Update the preset name in the sysex data
	for i := 0; i < 8; i++ {
		if i < len(newName) {
			data[8+i] = newName[i]
		} else {
			data[8+i] = 0x20 // space padding
		}
	}

	// Generate new filename
	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	dir := filepath.Dir(filePath)
	newFileName := fmt.Sprintf("%s_%s_%s.syx", category, newName, timeStr)
	newFilePath := filepath.Join(dir, newFileName)

	// Write the updated preset to the new file
	err = ioutil.WriteFile(newFilePath, data, 0644)
	if err != nil {
		log.Fatalf("failed to write renamed preset: %v", err)
	}

	// Remove the original file
	err = os.Remove(filePath)
	if err != nil {
		log.Printf("Warning: failed to remove original file '%s': %v", filePath, err)
	}

	fmt.Printf("Successfully renamed preset:\n")
	fmt.Printf("  Old: %s -> '%s' (%s)\n", filepath.Base(filePath), currentName, category)
	fmt.Printf("  New: %s -> '%s' (%s)\n", filepath.Base(newFilePath), newName, category)
}

func runChangeCategory(filePath string, newCatCode byte) {
	// Check if file exists
	if _, err := os.Stat(filePath); err != nil {
		log.Fatalf("failed to access file '%s': %v", filePath, err)
	}

	// Read the file
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}

	// Check if it's a single preset
	if len(data) != patchSize {
		log.Fatalf("file '%s' is not a single preset file (size: %d bytes, expected: %d)", filePath, len(data), patchSize)
	}

	// Extract current preset info
	currentName := strings.TrimRight(string(data[8:16]), " \x00")
	currentCatByte := data[16]
	currentCategory := getCategoryName(currentCatByte)
	newCategory := getCategoryName(newCatCode)

	fmt.Printf("Changing category of preset '%s' from %s to %s\n", currentName, currentCategory, newCategory)

	// Update the category in the sysex data
	data[16] = newCatCode

	// Generate new filename
	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	dir := filepath.Dir(filePath)
	newFileName := fmt.Sprintf("%s_%s_%s.syx", newCategory, currentName, timeStr)
	newFilePath := filepath.Join(dir, newFileName)

	// Write the updated preset to the new file
	err = ioutil.WriteFile(newFilePath, data, 0644)
	if err != nil {
		log.Fatalf("failed to write updated preset: %v", err)
	}

	// Remove the original file
	err = os.Remove(filePath)
	if err != nil {
		log.Printf("Warning: failed to remove original file '%s': %v", filePath, err)
	}

	fmt.Printf("Successfully changed category:\n")
	fmt.Printf("  Old: %s -> '%s' (%s)\n", filepath.Base(filePath), currentName, currentCategory)
	fmt.Printf("  New: %s -> '%s' (%s)\n", filepath.Base(newFilePath), currentName, newCategory)
}

func runRenameAndChangeCategory(filePath, newName string, newCatCode byte) {
	// Validate new name length
	if len(newName) > 8 {
		fmt.Printf("Warning: new name '%s' is longer than 8 characters, truncating to '%s'\n", newName, newName[:8])
		newName = newName[:8]
	}

	// Check if file exists
	if _, err := os.Stat(filePath); err != nil {
		log.Fatalf("failed to access file '%s': %v", filePath, err)
	}

	// Read the file
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}

	// Check if it's a single preset
	if len(data) != patchSize {
		log.Fatalf("file '%s' is not a single preset file (size: %d bytes, expected: %d)", filePath, len(data), patchSize)
	}

	// Extract current preset info
	currentName := strings.TrimRight(string(data[8:16]), " \x00")
	currentCatByte := data[16]
	currentCategory := getCategoryName(currentCatByte)
	newCategory := getCategoryName(newCatCode)

	fmt.Printf("Renaming preset '%s' (%s) to '%s' (%s)\n", currentName, currentCategory, newName, newCategory)

	// Update the preset name in the sysex data
	for i := 0; i < 8; i++ {
		if i < len(newName) {
			data[8+i] = newName[i]
		} else {
			data[8+i] = 0x20 // space padding
		}
	}

	// Update the category in the sysex data
	data[16] = newCatCode

	// Generate new filename
	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	dir := filepath.Dir(filePath)
	newFileName := fmt.Sprintf("%s_%s_%s.syx", newCategory, newName, timeStr)
	newFilePath := filepath.Join(dir, newFileName)

	// Write the updated preset to the new file
	err = ioutil.WriteFile(newFilePath, data, 0644)
	if err != nil {
		log.Fatalf("failed to write updated preset: %v", err)
	}

	// Remove the original file
	err = os.Remove(filePath)
	if err != nil {
		log.Printf("Warning: failed to remove original file '%s': %v", filePath, err)
	}

	fmt.Printf("Successfully renamed and changed category:\n")
	fmt.Printf("  Old: %s -> '%s' (%s)\n", filepath.Base(filePath), currentName, currentCategory)
	fmt.Printf("  New: %s -> '%s' (%s)\n", filepath.Base(newFilePath), newName, newCategory)
}

// suggestCategoryFromFile reads a preset file and suggests the current category
func suggestCategoryFromFile(filePath string) {
	// Check if file exists and is readable
	if _, err := os.Stat(filePath); err != nil {
		return // Can't help if file doesn't exist
	}

	// Try to read the file
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return // Can't help if can't read file
	}

	// Check if it's a single preset
	if len(data) != patchSize {
		return // Can't help if not a valid preset
	}

	// Extract current preset info
	currentName := strings.TrimRight(string(data[8:16]), " \x00")
	catByte := data[16]
	currentCategory := getCategoryName(catByte)

	fmt.Printf("Current preset: '%s' (%s)\n", currentName, currentCategory)
	fmt.Printf("Hint: use --change-category \"NewCategory\" to change from %s to another category.\n", currentCategory)
	printAvailableCategories()
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

		info, err := os.Stat(path)
		if err != nil {
			fmt.Printf("Warning: skipping '%s' - %v\n", path, err)
			continue
		}

		if info.IsDir() {
			// Include all .syx files in directory
			entries, err := ioutil.ReadDir(path)
			if err != nil {
				fmt.Printf("Warning: failed to read directory '%s': %v\n", path, err)
				continue
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				if strings.ToLower(filepath.Ext(e.Name())) != ".syx" {
					continue
				}
				validFiles = append(validFiles, filepath.Join(path, e.Name()))
			}
			continue
		}

		// Single file
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

func runExtract(path, extractList string) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}

	n := len(data) / patchSize
	if n <= 1 {
		fmt.Printf("File %s contains only %d preset(s), use single preset editing instead.\n", path, n)
		return
	}

	fmt.Printf("Extracting specific presets from %s (%d total presets):\n", path, n)

	// Extract existing names for position/name lookup
	existingNames := extractExistingNames(data)

	// Parse extraction targets
	targets := parseReplaceList(extractList, existingNames)

	if len(targets) == 0 {
		fmt.Println("Error: no valid extraction targets specified")
		return
	}

	// Warn for unmatched tokens
	warnUnmatchedTokens(extractList, targets)

	// Create output directory based on input filename
	baseName := strings.TrimSuffix(filepath.Base(path), ".syx")
	outputDir := filepath.Join("presets", baseName+"_extracted")
	err = os.MkdirAll(outputDir, 0755)
	if err != nil {
		log.Fatalf("failed to create output directory: %v", err)
	}

	timeStr := strconv.FormatInt(time.Now().Unix(), 10)
	extractedCount := 0

	// Extract each target preset
	for _, target := range targets {
		idx := target.index
		if idx < 0 || idx >= n {
			fmt.Printf("Warning: position %d out of range, skipping\n", idx+1)
			continue
		}

		// Extract preset data
		off := idx * patchSize
		presetData := data[off : off+patchSize]

		// Extract name and category
		name := strings.TrimRight(string(presetData[8:16]), " \x00")
		catByte := presetData[16]
		catName := getCategoryName(catByte)

		// Create filename: Category_PresetName_timestamp.syx
		filename := fmt.Sprintf("%s_%s_%s.syx", catName, name, timeStr)
		filePath := filepath.Join(outputDir, filename)

		// Write individual preset file
		err = ioutil.WriteFile(filePath, presetData, 0644)
		if err != nil {
			log.Printf("Warning: failed to write %s: %v", filePath, err)
			continue
		}

		fmt.Printf("  Extracted %2d: %s (%s) -> %s\n", idx+1, name, catName, filename)
		extractedCount++
	}

	if extractedCount > 0 {
		fmt.Printf("Extraction complete. %d preset files written to %s\n", extractedCount, outputDir)
	} else {
		fmt.Println("No presets were extracted.")
		// Remove empty directory
		os.Remove(outputDir)
	}
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
	// Write descriptor file
	if err := writeDescriptorFile(path, data); err != nil {
		log.Printf("Warning: %v", err)
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

// PresetReplacement holds information about a preset replacement operation
type PresetReplacement struct {
	Data     []byte
	Name     string
	Category string
}

// runEditWithFiles replaces specific presets with preset files
func runEditWithFiles(editFile, replaceList, replaceWithFiles string) {
	// Load and validate replacement files
	replacements, err := loadReplacementFiles(replaceWithFiles)
	if err != nil {
		log.Fatalf("failed to load replacement files: %v", err)
	}

	if len(replacements) == 0 {
		fmt.Println("Error: no valid single preset files found for replacement")
		return
	}

	// Show loaded replacements
	for i, replacement := range replacements {
		fmt.Printf("Loaded replacement preset %d: %s (%s)\n", i+1, replacement.Name, replacement.Category)
	}

	// Create preset generator function that cycles through loaded files
	generateReplacements := func(count int, nameExclusions map[string]struct{}) ([]PresetReplacement, error) {
		result := make([]PresetReplacement, count)
		for i := 0; i < count; i++ {
			replacementIndex := i % len(replacements)
			result[i] = replacements[replacementIndex]
		}
		return result, nil
	}

	// Use common edit logic
	runEditCommon(editFile, replaceList, generateReplacements)
}

// runEdit replaces patches with randomly generated ones
func runEdit(editFile, replaceList string, catCode byte, params map[string]ParamInfo, allowed map[string][]int, schema *gojsonschema.Schema) {
	// Create preset generator function for random generation
	generateReplacements := func(count int, nameExclusions map[string]struct{}) ([]PresetReplacement, error) {
		patches, names := generatePatchesWithExclusions(count, catCode, params, allowed, schema, nameExclusions)

		result := make([]PresetReplacement, count)
		for i := 0; i < count; i++ {
			categoryName := getCategoryName(catCode)
			result[i] = PresetReplacement{
				Data:     patches[i],
				Name:     names[i],
				Category: categoryName,
			}
		}
		return result, nil
	}

	// Use common edit logic
	runEditCommon(editFile, replaceList, generateReplacements)
}

// loadReplacementFiles loads and validates single preset files
func loadReplacementFiles(replaceWithFiles string) ([]PresetReplacement, error) {
	replaceFiles := strings.Split(replaceWithFiles, ",")
	var replacements []PresetReplacement

	for _, filePath := range replaceFiles {
		filePath = strings.TrimSpace(filePath)
		if filePath == "" {
			continue
		}

		// Check if file exists
		if _, err := os.Stat(filePath); err != nil {
			fmt.Printf("Warning: skipping '%s' - %v\n", filePath, err)
			continue
		}

		// Read file
		fileData, err := ioutil.ReadFile(filePath)
		if err != nil {
			fmt.Printf("Warning: failed to read '%s': %v\n", filePath, err)
			continue
		}

		// Check if it's a single preset
		if len(fileData) != patchSize {
			fmt.Printf("Warning: skipping '%s' - not a single preset file (size: %d bytes, expected: %d)\n",
				filePath, len(fileData), patchSize)
			continue
		}

		// Extract preset info
		name := strings.TrimRight(string(fileData[8:16]), " \x00")
		catByte := fileData[16]
		category := getCategoryName(catByte)

		replacement := PresetReplacement{
			Data:     fileData,
			Name:     name,
			Category: category,
		}

		replacements = append(replacements, replacement)
	}

	return replacements, nil
}

// PresetGenerator is a function type that generates replacement presets
type PresetGenerator func(count int, nameExclusions map[string]struct{}) ([]PresetReplacement, error)

// runEditCommon contains the shared logic for both edit modes
func runEditCommon(editFile, replaceList string, generateReplacements PresetGenerator) {
	data, err := os.ReadFile(editFile)
	if err != nil {
		log.Fatalf("failed to read sysex file: %v", err)
	}
	n := len(data) / patchSize

	// extract existing names
	existingNames := extractExistingNames(data)

	// parse replacement targets
	targets := parseReplaceList(replaceList, existingNames)

	if len(targets) == 0 {
		fmt.Println("Error: no valid replacement targets specified")
		return
	}

	// warn for unmatched tokens
	warnUnmatchedTokens(replaceList, targets)

	// create exclusion set for name generation
	nameExclusions := buildNameExclusions(existingNames, targets)

	// generate replacements
	replacements, err := generateReplacements(len(targets), nameExclusions)
	if err != nil {
		log.Fatalf("failed to generate replacements: %v", err)
	}

	// show what will be replaced
	showReplacementPlan(editFile, targets, existingNames, replacements)

	// check for name conflicts in final result
	checkNameConflicts(existingNames, targets, replacements)

	// apply replacements
	applyReplacements(data, targets, replacements, n)

	// write updated file
	err = os.WriteFile(editFile, data, 0644)
	if err != nil {
		log.Fatalf("failed to write sysex file: %v", err)
	}
	fmt.Printf("Successfully replaced %d presets in %s\n", len(targets), editFile)

	// write descriptor and show completion message
	updateDescriptorAndShowCompletion(editFile, data, n)
}

// warnUnmatchedTokens warns about replacement tokens that couldn't be matched
func warnUnmatchedTokens(replaceList string, targets []replaceTarget) {
	tokens := strings.Split(replaceList, ",")
	for _, tok := range tokens {
		t := strings.TrimSpace(tok)
		matched := false
		for _, tg := range targets {
			if tg.name != "" && strings.EqualFold(tg.name, t) {
				matched = true
				break
			}
			if tg.index >= 0 {
				pos := strconv.Itoa(tg.index + 1)
				if pos == t {
					matched = true
					break
				}
			}
		}
		if !matched {
			fmt.Printf("Warning: '%s' not found as preset name or position\n", t)
		}
	}
}

// buildNameExclusions creates a set of existing names to avoid conflicts
func buildNameExclusions(existingNames []string, targets []replaceTarget) map[string]struct{} {
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

	return nameExclusions
}

// showReplacementPlan displays what will be replaced
func showReplacementPlan(editFile string, targets []replaceTarget, existingNames []string, replacements []PresetReplacement) {
	fmt.Printf("Replacing %d presets in %s:\n", len(targets), editFile)
	for i, target := range targets {
		oldName := ""
		if target.index >= 0 && target.index < len(existingNames) {
			oldName = existingNames[target.index]
		}
		fmt.Printf("  Position %d: '%s' -> '%s' (%s)\n",
			target.index+1, oldName, replacements[i].Name, replacements[i].Category)
	}
}

// checkNameConflicts warns about duplicate names in the final result
func checkNameConflicts(existingNames []string, targets []replaceTarget, replacements []PresetReplacement) {
	finalNames := make([]string, len(existingNames))
	copy(finalNames, existingNames)

	// Update final names with replacements
	for i, target := range targets {
		finalNames[target.index] = replacements[i].Name
	}

	// Check for conflicts
	nameConflicts := make(map[string][]int) // name -> list of positions where it appears
	for i, name := range finalNames {
		lowerName := strings.ToLower(name)
		nameConflicts[lowerName] = append(nameConflicts[lowerName], i+1)
	}

	hasConflicts := false
	for lowerName, positions := range nameConflicts {
		if len(positions) > 1 {
			if !hasConflicts {
				fmt.Println("Warning: detected name conflicts in the final result:")
				hasConflicts = true
			}
			// Find the original name (case-sensitive) for display
			originalName := ""
			for _, fname := range finalNames {
				if strings.ToLower(fname) == lowerName {
					originalName = fname
					break
				}
			}
			fmt.Printf("  '%s' will appear at positions: %v\n", originalName, positions)
		}
	}
	if hasConflicts {
		fmt.Println("Proceeding anyway - duplicates will be preserved")
	}
}

// applyReplacements applies the replacement presets to the data
func applyReplacements(data []byte, targets []replaceTarget, replacements []PresetReplacement, n int) {
	for i, target := range targets {
		idx := target.index
		if idx < 0 || idx >= n {
			fmt.Printf("Warning: position %d out of range\n", idx+1)
			continue
		}

		off := idx * patchSize
		copy(data[off:off+patchSize], replacements[i].Data)
	}
}

// updateDescriptorAndShowCompletion handles final file updates and messaging
func updateDescriptorAndShowCompletion(editFile string, data []byte, n int) {
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
