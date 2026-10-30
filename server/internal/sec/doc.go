// Package sec reads SEC EDGAR: the ticker map, the submissions of each
// company, company facts, the daily filing index and the bulk archives. The
// Client fetches files and obeys the SEC access rules. The Parse and Read
// functions are pure, so a reprocess can run them on stored responses.
package sec
