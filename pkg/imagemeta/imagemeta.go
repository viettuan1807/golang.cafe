package imagemeta

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"path/filepath"

	"github.com/0x13a/golang.cafe/pkg/database"
	"github.com/fogleman/gg"
	"github.com/pkg/errors"
)

const (
	backgroundImageFilename = "static/assets/img/meta-bg.jpg"
	outputFilename          = "output.jpg"
)

func GenerateImageForJob(job database.JobPost) (io.ReadWriter, error) {
	dc := gg.NewContext(1200, 628)
	backgroundImage, err := gg.LoadImage(backgroundImageFilename)
	w := bytes.NewBuffer([]byte{})
	if err != nil {
		return w, errors.Wrap(err, "load background image")
	}
	// draw background image
	dc.DrawImage(backgroundImage, 0, 0)

	// draw job link text
	textColor := color.Black
	fontPath := filepath.Join("static", "assets", "fonts", "Courier_Prime", "CourierPrime-Regular.ttf")
	if err := dc.LoadFontFace(fontPath, 16); err != nil {
		return w, errors.Wrap(err, "load Courier_Prime for job link")
	}
	dc.SetColor(textColor)
	var marginY float64
	marginY = 80
	s := fmt.Sprintf("https://golang.cafe/job/%s", job.Slug)
	_, textHeight := dc.MeasureString(s)
	var x, y float64
	x = 70
	y = float64(dc.Height()) - textHeight - marginY
	dc.DrawString(s, x, y)

	// draw job title and description
	title := fmt.Sprintf("%s with %s\n\n %s\n\n %s", job.JobTitle, job.Company, job.Location, job.SalaryRange)
	mainTextColor := color.RGBA{
		R: uint8(0),
		G: uint8(0),
		B: uint8(144),
		A: uint8(255),
	}
	fontPath = filepath.Join("static", "assets", "fonts", "Courier_Prime", "CourierPrime-Bold.ttf")
	if err := dc.LoadFontFace(fontPath, 60); err != nil {
		return w, errors.Wrap(err, "load Courier_Prime for job link")
	}
	textRightMargin := 80.0
	textTopMargin := 90.0
	x = textRightMargin
	y = textTopMargin
	maxWidth := float64(dc.Width()) - textRightMargin - textRightMargin
	dc.SetColor(mainTextColor)
	dc.DrawStringWrapped(title, x, y, 0, 0, maxWidth, 1.5, gg.AlignLeft)

	if err := png.Encode(w, dc.Image()); err != nil {
		return w, err
	}

	return w, nil
}
