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

