// Package sec reads SEC EDGAR: the ticker map, the submissions of each
// company, company facts, the daily filing index and the bulk company facts
// archive. The Client fetches files and obeys the SEC access rules. The
// Parse and Read functions are pure, so a reprocess can run them on stored
// responses.
package sec
