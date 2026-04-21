package entity

type SearchResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Duration  int    `json:"duration"`
	Thumbnail string `json:"thumbnail"`
	URL       string `json:"url"`
}
