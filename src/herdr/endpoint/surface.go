package endpoint

import "errors"

// ErrStalePatch is a patch of another revision of the surface than the
// client holds; the server sends a whole surface after it.
var ErrStalePatch = errors.New("endpoint: patch of another surface revision")

// Apply changes s by p, or leaves it as it was and returns an error.
func (s *Surface) Apply(p *SurfacePatch) error {
	if p.BootID != s.BootID || p.ProjectionRevision != s.ProjectionRevision ||
		p.BaseRevision != s.SurfaceRevision || p.Revision != s.SurfaceRevision+1 || s.Popup != nil {
		return ErrStalePatch
	}
	f := &s.Frame
	for _, row := range p.Rows {
		if row.Y >= f.Height || int(row.X)+len(row.Cells) > int(f.Width) {
			return errMalformed
		}
	}
	for _, row := range p.Rows {
		copy(f.Cells[int(row.Y)*int(f.Width)+int(row.X):], row.Cells)
	}
	for _, pane := range p.Panes {
		for i := range s.Panes {
			if s.Panes[i].ID == pane.ID {
				s.Panes[i] = pane
			}
		}
	}
	f.Cursor = p.Cursor
	s.SurfaceRevision = p.Revision
	return nil
}

// Row returns the text of a row of the frame.
func (f *Frame) Row(y int) string {
	var b []byte
	for _, c := range f.Cells[y*int(f.Width) : (y+1)*int(f.Width)] {
		if !c.Skip {
			b = append(b, c.Symbol...)
		}
	}
	return string(b)
}
