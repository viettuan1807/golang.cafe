package main

import (
	"fmt"

	"github.com/gocolly/colly"
)

func main() {
	c := colly.NewCollector()
	
	c.OnHTML(".listResults", func(e *colly.HTMLElement) {
		fmt.Printf("First column of a table row: %v", e)
	})
	
	c.OnScraped(func(r *colly.Response) {
		fmt.Println("Finished", r.Request.URL)
	})

	c.Visit("https://stackoverflow.com/jobs?tl=go&s=1&c=GBP")
}
