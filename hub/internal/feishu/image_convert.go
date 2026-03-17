package feishu

import (
	"bytes"
	"fmt"
	"image"

	_ "image/gif"  // register GIF decoder
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder

	_ "golang.org/x/image/bmp"  // register BMP decoder
	_ "golang.org/x/image/tiff" // register TIFF decoder
	_ "golang.org/x/image/webp" // register WebP decoder
)

// decodeAnyImage decodes raw image bytes in any registered format (png, jpeg,
// gif, webp, bmp, tiff) and returns the decoded image.Image along with the
// detected format name. The returned image.Image can be passed directly to
// go-lark's UploadImageObject which re-encodes it as JPEG internally.
func decodeAnyImage(data []byte) (image.Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("image decode (%d bytes): %w", len(data), err)
	}
	return img, format, nil
}
