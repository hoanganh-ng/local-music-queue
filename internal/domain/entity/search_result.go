package entity

type SearchResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Duration  int    `json:"duration"`
	Thumbnail string `json:"thumbnail"`
	URL       string `json:"url"`
	// AddedBy and AddedByID are stamped server-side by the room queue
	// handler from the bearer-resolved actor so the queue records who
	// added each song. They are NEVER read from the wire; the json:"-"
	// tags keep them out of the request shape so a client cannot spoof
	// attribution.
	AddedBy   string `json:"-"`
	AddedByID int    `json:"-"`
}
