package upload

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"pilotserver/internal/store"
)

func pruneUploads(st *store.Store, dataDir string, keep store.Segment) {
	if st == nil {
		return
	}
	policy, err := st.UploadPolicy()
	if err != nil {
		log.Printf("upload policy: %v", err)
		return
	}
	if policy.RetentionDays > 0 {
		expired, err := st.ListSegmentsUploadedBefore(store.RetentionCutoff(time.Now(), policy.RetentionDays))
		if err != nil {
			log.Printf("list expired uploads: %v", err)
			return
		}
		for _, segment := range expired {
			removeUpload(dataDir, st, segment)
		}
	}
	if policy.MaxBytes <= 0 {
		return
	}
	for {
		total, err := st.TotalUploadBytes()
		if err != nil {
			log.Printf("upload total: %v", err)
			return
		}
		if total <= policy.MaxBytes {
			return
		}
		oldest, ok, err := st.OldestSegmentExcept(keep.DongleID, keep.RelPath)
		if err != nil {
			log.Printf("oldest upload: %v", err)
			return
		}
		if !ok {
			return
		}
		removeUpload(dataDir, st, oldest)
	}
}

func removeUpload(dataDir string, st *store.Store, segment store.Segment) {
	path := filepath.Join(dataDir, "uploads", segment.DongleID, filepath.FromSlash(segment.RelPath))
	_ = os.Remove(path)
	_ = os.Remove(filepath.Dir(path))
	_ = os.Remove(filepath.Dir(filepath.Dir(path)))
	if err := st.DeleteSegment(segment.DongleID, segment.RelPath); err != nil {
		log.Printf("delete upload %s: %v", segment.RelPath, err)
	}
}
