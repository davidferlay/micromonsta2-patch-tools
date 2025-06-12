# Micromonsta 2 Patch Tools

A comprehensive CLI tool for managing **Micromonsta 2** hardware synthesizer patch presets. Generate, edit, split, group, and describe `.syx` (SysEx) patch files with schema validation and collision prevention.

---

## ✨ Features

- 🎲 **Generate** randomized, schema-compliant patches by category (Bass, Lead, Pad, etc.)
- ✏️ **Edit** existing bundles by replacing specific presets by position or name
- 🔍 **Describe** patch contents to see what's inside any `.syx` file
- ✂️ **Split** multi-preset bundles into individual preset files
- 🔗 **Group** multiple `.syx` files (single presets or bundles) into one bundle
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
# Replace presets by position
micromonsta2-patch-tools --edit bundle.syx --replace "1,3,5" --category Lead

# Replace presets by name (case-insensitive)
micromonsta2-patch-tools --edit bundle.syx --replace "happy,sad" --category Pad

# Mix positions and names
micromonsta2-patch-tools --edit bundle.syx --replace "1,warm,3" --category Keys
```

### Describe Patch Contents

```bash
# See what's inside any .syx file
micromonsta2-patch-tools --describe bundle.syx
```

### Split Bundles

```bash
# Split a multi-preset bundle into individual files
micromonsta2-patch-tools --split bundle.syx
```

### Group Files Together

```bash
# Group multiple files into a new bundle
micromonsta2-patch-tools --group "preset1.syx,preset2.syx,bundle.syx"
```

### Command Line Arguments

| Flag           | Description                                                      |
| -------------- | ---------------------------------------------------------------- |
| `--category`   | (Required for generate/edit) Patch category: Lead, Bass, Pad, Keys, Organ, String, Brass, Percussion, Drone, Noise, SFX, Arp, Misc, User1, User2, User3 |
| `--count`      | (Optional) Number of unique patches to generate. Default: `1`    |
| `--specs`      | (Optional) Path to custom spec directory. Default: `specs`      |
| `--edit`       | Path to existing `.syx` file to edit                            |
| `--replace`    | Comma-separated list of preset positions (1-based) or names to replace |
| `--describe`   | Path to `.syx` file to describe contents                        |
| `--split`      | Path to `.syx` file to split into individual preset files       |
| `--group`      | Comma-separated list of `.syx` files to group into a bundle     |

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

# 4. Replace a few presets
micromonsta2-patch-tools --edit presets/MyBundle/mybundle_grouped_*.syx --replace "2,5" --category Pad

# 5. Split if needed
micromonsta2-patch-tools --split presets/MyBundle/mybundle_grouped_*.syx
```

### Edit Existing Bundles
```bash
# See what's in your bundle
micromonsta2-patch-tools --describe my_bundle.syx
# Output:
# 1: happy (Lead)
# 2: warm (Bass)
# 3: bright (Pad)

# Replace specific presets
micromonsta2-patch-tools --edit my_bundle.syx --replace "warm,3" --category Keys
# This replaces "warm" and position 3 with new Keys presets
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

