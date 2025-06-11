
# Micromonsta 2 Patch Generator

Generate randomized, schema-compliant patch presets for the **Micromonsta 2** hardware synthesizer. This CLI tool outputs one or more presets in `.syx` (SysEx) format, ready to be transferred to your synth.

---

## ✨ Features

- ⚙️ Validates parameters against a comprehensive JSON schema.
- 🎲 Generates unique randomized patches per category (Bass, Lead, Pad, etc.).
- 📁 Bundles patches in `.syx` files for easy transfer.
- 🧠 Uses embedded specs and schema by default; supports custom overrides.

---

## 🚀 Installation

```bash
 go build .
````

---

## 🛠️ Usage

```bash
micromonsta2-patch-tools --category Lead
# or
go run micromonsta2-patch-tools --category Bass --specs specs-var --count 10
```

### Arguments

| Flag         | Description                                                |
| ------------ | ---------------------------------------------------------- |
| `--category` | (Required) Patch category: Lead, Bass, Pad, Keys, etc.     |
| `--count`    | (Optional) Number of unique patches to generate. Default: `1`         |
| `--specs`    | (Optional) Path to custom spec directory. Default: `specs` |

Example output:

```
Wrote combined 10 presets to presets/Groovy_Lead_bundle_1720000000.syx
Wrote 10 individual presets to presets/Groovy_Lead_bundle/
```

---

## 📁 Output

* A new directory `presets/` will be created.
* If `--count > 1`, a combined `.syx` and individual `.syx` files will be written.
* Each patch includes:

  * A unique name
  * Proper Sysex encoding
  * Embedded category byte

---

## 🎹 Requirements

* Micromonsta 2 firmware compatible with patch format.
* MIDI SysEx transfer tool (e.g. [SysEx Librarian](https://www.snoize.com/SysExLibrarian/), [MIDI-OX](http://www.midiox.com/)) to load the `.syx` files to your synth.

---

## 🧪 JSON Schema & Specs

* `micromonsta_patch_schema.json` defines valid parameter ranges and types.
* `specs/*.json` files provide category-specific parameter metadata.
* You can extend or replace these with your own files using `--specs`.

---

## 📜 License

MIT — use freely, modify safely, share joyfully.

---

## 🙏 Credits

* Inspired by [Micromonsta 2](https://www.audiothingies.com/product/micromonsta-2/)
* Uses [`gojsonschema`](https://github.com/xeipuuv/gojsonschema) and [`go-randomdata`](https://github.com/Pallinder/go-randomdata)


