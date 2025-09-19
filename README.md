# thumbgrid

Visual terminal grid selector built on the kitty protocol

## Install

- **Requires:** Go 1.23+

```bash
go install github.com/ck-zhang/thumbgrid/cmd/thumbgrid@latest
```

### Thumbnail helpers

Thumbgrid depends on the following packages

- `ffmpeg` for video thumbnails
- `vipsthumbnail` from libvips for fast image thumbnails
- `magick` as fallback

If more than one tool is available, Thumbgrid picks the best match automatically

## Usage

```bash
thumbgrid [PATH]

# examples
thumbgrid ~/Pictures
thumbgrid -filter video ~/Videos
thumbgrid -sort size -order desc .
```

| Option    | Values                       |
| --------- | ---------------------------- |
| `-filter` | `image` \| `video` \| `both` |
| `-sort`   | `name`  \| `mtime` \| `size` |
| `-order`  | `asc`   \| `desc`            |


**Keys**

- Move: arrows / `h j k l`
- Page: PgUp/PgDn or `Ctrl-B`/`Ctrl-F`
- Jump: `g g` (top), `G` (bottom)
- View: `p` toggle previews, `+`/`-` tile size
- Confirm: **Enter** · Cancel: `q`/`Esc`
- Mouse & scroll supported when available

## Example lf integration

Drop the snippet below into `~/.config/lf/lfrc` to launch thumbgrid with `Ctrl-t` from the current lf directory and apply the selection back to lf.

```sh
# ~/.config/lf/lfrc
cmd thumbgrid ${{
    tmp="$(mktemp)"
    dir="${LF_PWD:-$PWD}"
    trap 'rm -f "$tmp"' EXIT
    if [ -d "$dir" ]; then
        cd "$dir" || exit 1
    fi
    if THUMBGRID_SELECTION_FILE="$tmp" thumbgrid "$dir"; then
        if [ -s "$tmp" ]; then
            lf -remote "send $id load"
            first=""
            while IFS= read -r abs; do
                [ -z "$abs" ] && continue
                rel="$abs"
                if command -v realpath >/dev/null 2>&1; then
                    rel="$(realpath --relative-to="$PWD" "$abs" 2>/dev/null || printf '%s' "$abs")"
                fi
                escaped=$(printf '%q' "$rel")
                lf -remote "send $id select $escaped"
                if [ -z "$first" ]; then
                    first="$escaped"
                fi
            done <"$tmp"
            if [ -n "$first" ]; then
                lf -remote "send $id open $first"
            fi
        fi
    fi
}}
map <c-t> thumbgrid
```
