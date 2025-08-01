// Code generated by go-swagger; DO NOT EDIT.

package volume

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

// ListResponse VolumeListResponse
//
// # Volume list response
//
// swagger:model ListResponse
type ListResponse struct {

	// List of volumes
	Volumes []*Volume `json:"Volumes"`

	// Warnings that occurred when fetching the list of volumes.
	//
	// Example: []
	Warnings []string `json:"Warnings"`
}
