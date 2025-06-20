# Micromonsta 2 Patch Tools

A comprehensive CLI tool for managing **Micromonsta 2** hardware synthesizer patch presets. Generate, edit, split, group, sort, and describe `.syx` (SysEx) patch files with schema validation and collision prevention.

---

## ✨ Features

- 🎲 **Generate** randomized, schema-compliant patches by category (Bass, Lead, Pad, etc.)
- ✏️ **Edit** existing bundles by replacing specific presets by position or name (with random generation or specific preset files)
- 🔍 **Describe** patch contents to see what's inside any `.syx` file
- ✂️ **Split** multi-preset bundles into individual preset files
- 🎯 **Extract** specific presets from bundles by position or name
- 🔗 **Group** multiple `.syx` files (single presets or bundles) into one bundle
- 🔄 **Sort** presets in bundles by category then alphabetically
- 🏷️ **Rename** individual presets (updates both SysEx data and filename)
- 📂 **Change category** of individual presets (updates category in SysEx data and filename)
- 📁 **Bundle management** with automatic descriptor files for multi-preset collections
- 🛡️ **Name collision prevention** when editing existing bundles
- ⚙️ **Schema validation** against comprehensive JSON parameter constraints
- 🧠 **Embedded specs** with support for custom overrides

---

## 🚀 Installation

```bash
go build .
```

---

## 🛠️ Usage

### Generate New Presets

```bash
# Generate a single preset
micromonsta2-patch-tools --category Lead --count 1

# Generate a bundle of 10 presets
micromonsta2-patch-tools --category Bass --count 10
```

### Edit Existing Bundles

```bash
# Replace presets by position with randomly generated presets
micromonsta2-patch-tools --edit bundle.syx --replace "1,3,5" --category Lead

# Replace presets by name with randomly generated presets (case-insensitive)
micromonsta2-patch-tools --edit bundle.syx --replace "happy,sad" --category Pad

# Mix positions and names with randomly generated presets
micromonsta2-patch-tools --edit bundle.syx --replace "1,warm,3" --category Keys

# Replace presets with specific single preset files
micromonsta2-patch-tools --edit bundle.syx --replace "1,3,5" --replace-with "preset1.syx,preset2.syx,preset3.syx"

# Replace by name with specific preset files
micromonsta2-patch-tools --edit bundle.syx --replace "warm,bright" --replace-with "my_bass.syx,cool_lead.syx"

# If you have fewer replacement files than targets, files will be cycled
micromonsta2-patch-tools --edit bundle.syx --replace "1,2,3,4" --replace-with "preset1.syx,preset2.syx"
# This will use: preset1.syx, preset2.syx, preset1.syx, preset2.syx
```

### Rename Individual Presets

```bash
# Rename a single preset
micromonsta2-patch-tools --edit Lead_bright_1720000000.syx --rename "killer"

# The tool will update both the internal preset name and create a new file
# Old: Lead_bright_1720000000.syx -> 'bright' (Lead)
# New: Lead_killer_1720000001.syx -> 'killer' (Lead)
```

The rename feature will:
- Update the preset name inside the SysEx data
- Create a new file with the updated name in the filename
- Remove the original file (clean replacement)
- Automatically truncate names longer than 8 characters (hardware limitation)
- Maintain the original category

### Change Category of Individual Presets

```bash
# Change a preset's category
micromonsta2-patch-tools --edit Bass_wobble_1720000000.syx --change-category "Lead"

# The tool will update the category in SysEx data and create a new file
# Old: Bass_wobble_1720000000.syx -> 'wobble' (Bass)
# New: Lead_wobble_1720000001.syx -> 'wobble' (Lead)

# You can also rename and change category at the same time
micromonsta2-patch-tools --edit Bass_wobble_1720000000.syx --rename "awesome" --change-category "Lead"
# Old: Bass_wobble_1720000000.syx -> 'wobble' (Bass)
# New: Lead_awesome_1720000001.syx -> 'awesome' (Lead)
```

The category change feature will:
- Update the category byte inside the SysEx data
- Create a new file with the updated category in the filename
- Remove the original file (clean replacement)
- Maintain the original preset name (unless also using --rename)
- Support all available categories: Bass, Lead, Pad, Keys, Organ, String, Brass, Percussion, Drone, Noise, SFX, Arp, Misc, User1, User2, User3

### Sort Presets in Bundles

```bash
# Sort presets by category then alphabetically
micromonsta2-patch-tools --sort my_bundle.syx
```

The sort feature will:
- Display current preset order
- Sort by category (Bass → Lead → Pad → Keys → Organ → String → Brass → Percussion → Drone → Noise → SFX → Arp → Misc → User1 → User2 → User3 → Unknown)
- Within each category, sort alphabetically by preset name (case-insensitive)
- Create a backup file before modifying
- Update the descriptor file
- Show the new order and number of presets that moved

### Describe Patch Contents

```bash
# See what's inside any .syx file
micromonsta2-patch-tools --describe bundle.syx
```

### Split Bundles

```bash
# Split a multi-preset bundle into individual files (all presets)
micromonsta2-patch-tools --split bundle.syx

# Extract specific presets by position
micromonsta2-patch-tools --split bundle.syx --extract "1,3,5"

# Extract specific presets by name (case-insensitive)
micromonsta2-patch-tools --split bundle.syx --extract "warm,bright,deep"

# Mix positions and names
micromonsta2-patch-tools --split bundle.syx --extract "1,warm,5,bright"
```

**Split vs Extract:**
- `--split` alone: Extracts ALL presets from the bundle
- `--split` with `--extract`: Extracts only the specified presets

The extract feature will:
- Only work with bundle files (2+ presets)
- Create individual preset files for selected presets only
- Support both position numbers (1-based) and preset names (case-insensitive)
- Create an output directory named `OriginalName_extracted`
- Warn about invalid positions or names that can't be found

### Group Files Together

```bash
# Group multiple files into a new bundle
micromonsta2-patch-tools --group "preset1.syx,preset2.syx,bundle.syx"
micromonsta2-patch-tools --group dirname/
```

### Command Line Arguments

| Flag           | Description                                                      |
| -------------- | ---------------------------------------------------------------- |
| `--category`   | (Required for generate/edit with random presets) Patch category: Lead, Bass, Pad, Keys, Organ, String, Brass, Percussion, Drone, Noise, SFX, Arp, Misc, User1, User2, User3 |
| `--count`      | (Optional) Number of unique patches to generate. Default: `1`    |
| `--specs`      | (Optional) Path to custom spec directory. Default: `specs`      |
| `--edit`       | Path to existing `.syx` file to edit                            |
| `--replace`    | Comma-separated list of preset positions (1-based) or names to replace |
| `--replace-with` | Comma-separated list of single preset `.syx` files to use as replacements |
| `--describe`   | Path to `.syx` file to describe contents                        |
| `--split`      | Path to `.syx` file to split into individual preset files       |
| `--extract`    | Comma-separated list of preset positions (1-based) or names to extract from bundle |
| `--group`      | Comma-separated list of `.syx` files or directories to group into a bundle     |
| `--sort`       | Path to `.syx` file to sort presets by category then alphabetically |

---

## 📁 Output Structure

### Single Preset
```
presets/
└── Lead_bright_1720000000.syx
```

### Bundle (Multiple Presets)
```
presets/
└── Happy/
    ├── happy_bundle_1720000000.syx          # Combined bundle
    ├── happy_bundle_1720000000.txt          # Descriptor file
    ├── Lead_bright_1720000000.syx           # Individual presets
    ├── Lead_warm_1720000000.syx
    └── Bass_deep_1720000000.syx
```

### Split Output
```
presets/
└── OriginalBundle_split/
    ├── Lead_bright_1720000000.syx
    ├── Bass_deep_1720000000.syx
    └── Pad_warm_1720000000.syx
```

### Extract Output
```
presets/
└── OriginalBundle_extracted/
    ├── Lead_bright_1720000000.syx           # Only extracted presets
    └── Pad_warm_1720000000.syx
```

### Group Output
```
presets/
└── Grouped/
    ├── grouped_grouped_1720000000.syx       # Combined bundle
    ├── grouped_grouped_1720000000.txt       # Descriptor file
    └── [individual preset files...]
```

---

## 📋 Example Workflows

### Create and Manage a Custom Bundle
```bash
# 1. Generate some presets
micromonsta2-patch-tools --category Lead --count 5
micromonsta2-patch-tools --category Bass --count 3

# 2. Group them together
micromonsta2-patch-tools --group "presets/Lead_*.syx,presets/Bass_*.syx"

# 3. Check what's inside
micromonsta2-patch-tools --describe presets/MyBundle/mybundle_grouped_*.syx

# 4. Sort presets by category and name
micromonsta2-patch-tools --sort presets/MyBundle/mybundle_grouped_*.syx

# 5. Replace a few presets with random ones
micromonsta2-patch-tools --edit presets/MyBundle/mybundle_grouped_*.syx --replace "2,5" --category Pad

# 6. Or replace with specific preset files
micromonsta2-patch-tools --edit presets/MyBundle/mybundle_grouped_*.syx --replace "1,3" --replace-with "my_favorite.syx,another_great.syx"

# 7. Split if needed
micromonsta2-patch-tools --split presets/MyBundle/mybundle_grouped_*.syx
```

### Replace Presets with Specific Files
```bash
# See what you have
micromonsta2-patch-tools --describe my_bundle.syx
# Output:
# 1: oldlead (Lead)
# 2: oldbass (Bass)
# 3: oldpad (Pad)

# Replace specific positions with your own presets
micromonsta2-patch-tools --edit my_bundle.syx --replace "1,3" --replace-with "perfect_lead.syx,amazing_pad.syx"

# Replace by name
micromonsta2-patch-tools --edit my_bundle.syx --replace "oldbass" --replace-with "killer_bass.syx"

# Replace multiple presets with cycling through fewer files
micromonsta2-patch-tools --edit my_bundle.syx --replace "1,2,3,4" --replace-with "preset_a.syx,preset_b.syx"
# Will use: preset_a, preset_b, preset_a, preset_b
```

### Organize and Manage Individual Presets
```bash
# Rename a preset for better organization
micromonsta2-patch-tools --edit Bass_adjective_1720000000.syx --rename "wobble"

# Change a preset's category
micromonsta2-patch-tools --edit Bass_wobble_1720000001.syx --change-category "Lead"

# Do both at once
micromonsta2-patch-tools --edit Lead_wobble_1720000002.syx --rename "awesome" --change-category "Pad"

# Generate some presets
micromonsta2-patch-tools --category Lead --count 3

# Organize them by renaming and categorizing
micromonsta2-patch-tools --edit Lead_random1_*.syx --rename "arp1" --change-category "Arp"
micromonsta2-patch-tools --edit Lead_random2_*.syx --rename "arp2" --change-category "Arp"
micromonsta2-patch-tools --edit Lead_random3_*.syx --rename "arp3" --change-category "Arp"

# Group them into a bundle
micromonsta2-patch-tools --group "Arp_arp1_*.syx,Arp_arp2_*.syx,Arp_arp3_*.syx"
```

### Organize Existing Bundles
```bash
# See current organization
micromonsta2-patch-tools --describe messy_bundle.syx
# Output might show:
# 1: warm (Pad)
# 2: deep (Bass)
# 3: bright (Lead)
# 4: cool (Bass)
# 5: soft (Pad)

# Sort to organize better
micromonsta2-patch-tools --sort messy_bundle.syx
# New order will be:
# 1: cool (Bass)
# 2: deep (Bass)
# 3: bright (Lead)
# 4: soft (Pad)
# 5: warm (Pad)
```

### Edit Existing Bundles
```bash
# See what's in your bundle
micromonsta2-patch-tools --describe my_bundle.syx
# Output:
# 1: happy (Lead)
# 2: warm (Bass)
# 3: bright (Pad)

# Replace with randomly generated presets
micromonsta2-patch-tools --edit my_bundle.syx --replace "warm,3" --category Keys
# This replaces "warm" and position 3 with new random Keys presets

# Replace with specific preset files
micromonsta2-patch-tools --edit my_bundle.syx --replace "1,warm" --replace-with "my_lead.syx,perfect_bass.syx"
# This replaces position 1 with my_lead.syx and "warm" with perfect_bass.syx

# Rename a single preset
micromonsta2-patch-tools --edit Lead_bright_1720000000.syx --rename "killer"
# This renames the preset internally and creates a new file

# Change category of a single preset
micromonsta2-patch-tools --edit Bass_wobble_1720000000.syx --change-category "Lead"
# This changes the category and creates a new file

# Rename and change category at the same time
micromonsta2-patch-tools --edit Bass_old_1720000000.syx --rename "new" --change-category "Pad"
# This does both operations and creates a new file
```

---

## 🎹 Synthesizer Compatibility

- **Micromonsta 2** firmware compatible with patch format
- Use MIDI SysEx transfer tools to load `.syx` files:
  - [SysEx Librarian](https://www.snoize.com/SysExLibrarian/) (macOS)
  - [MIDI-OX](http://www.midiox.com/) (Windows)
  - [SendMIDI](https://github.com/gbevin/SendMIDI) (Cross-platform)

---

## 🧪 Technical Details

### JSON Schema & Specs
- `micromonsta_patch_schema.json` defines valid parameter ranges and constraints
- `specs/*.json` files provide category-specific parameter metadata
- Custom specs can be provided with `--specs` flag

### Name Collision Prevention
- When editing bundles, new preset names won't conflict with existing ones
- Case-insensitive duplicate detection
- Multiple replacements in one session are handled safely

### Sort Algorithm
- **Primary sort**: By category in a predefined order (Bass → Lead → Pad → etc.)
- **Secondary sort**: Alphabetically by preset name (case-insensitive)
- **Stable sort**: Presets with identical names maintain their original relative order
- **Backup**: Creates a timestamped backup before modifying the original file

### File Format
- Standard MIDI SysEx format
- 176 bytes per patch
- Embedded category and name information
- Compatible with Micromonsta 2 hardware

---

## 📜 License

MIT — use freely, modify safely, share joyfully.

---

## 🙏 Credits

- Inspired by [Micromonsta 2](https://www.audiothingies.com/product/micromonsta-2/)
- Uses [`gojsonschema`](https://github.com/xeipuuv/gojsonschema) and [`go-randomdata`](https://github.com/Pallinder/go-randomdata)
