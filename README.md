gopakgen
========

gopakgen generates a list of Flatpak manifest sources for a given Go module and prints them to stdout. For example,

```bash
$ gopakgen deedles.dev/gopakgen # Prints the sources for the latest version of this package.
$ gopakgen tailscale.com@v1.16.2 # Prints the sources for version v1.16.2 of tailscale.com.
```

To use the list, simply pipe it into a file and import it into your manifest:

```bash
$ gopakgen deedles.dev/trayscale > trayscale.deps.json
```

#### `manifest.yml`
```yml
modules:
  - name: example
    buildsystem: simple
    sources:
    - trayscale.deps.json
```
