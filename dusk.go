package celeritas

import (
	"fmt"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/rod/lib/utils"
	"time"
)

func (c *Celeritas) TakeScreenShot(pageURL, testName string, w, h float64) {
	page := rod.New().MustConnect().MustIgnoreCertErrors(true).MustPage(pageURL).MustWaitLoad()

	img, _ := page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
		Clip: &proto.PageViewport{
			X: 0,
			Y: 0,
			Width: w,
			Height: h,
			Scale: 1,
		},
		FromSurface: true,
	})
	fileName := time.Now().Format("2006-01-02-15-04-05.000000")
	_ = utils.OutputFile(fmt.Sprintf("%s/screenshots/%s-%s.png", c.RootPath, testName, fileName), img)
}

func (c *Celeritas) FetchPage(pageURL string) *rod.Page {
	return rod.New().MustConnect().MustIgnoreCertErrors(true).MustPage(pageURL).MustWaitLoad()
}

func (c *Celeritas) SelectElementByID(page *rod.Page, id string) *rod.Element {
	return page.MustElementByJS(fmt.Sprintf("document.getElementById('%s')", id))
}