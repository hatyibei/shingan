package application

import "github.com/hatyibei/shingan/domain"

// ReportFormatter converts a list of Findings into a formatted byte slice.
// Implementations live in the infrastructure layer (Onion Architecture dependency inversion).
type ReportFormatter interface {
	// Format serializes findings into the implementation-specific format.
	Format(findings []domain.Finding) ([]byte, error)

	// ContentType returns the MIME type of the formatted output
	// (e.g. "application/json", "text/markdown").
	ContentType() string
}
