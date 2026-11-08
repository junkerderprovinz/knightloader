// Package uploadrar is Uploadrar on the XFileSharing client. Its pages keep
// to the script's own template, with an advert labelled as the download link
// on the file page, which the client's link patterns pass over.
//
// The patterns follow the UploadrarCom plugin of JDownloader (GPLv3).
package uploadrar

import "github.com/junkerderprovinz/knightloader/internal/resolver/xfs"

// Site is Uploadrar.
var Site = &xfs.Site{
	ID:           "uploadrar",
	Name:         "Uploadrar",
	Host:         "uploadrar.com",
	OwnLogin:     true,
	FreeConns:    1,
	PremiumConns: 4,
}
