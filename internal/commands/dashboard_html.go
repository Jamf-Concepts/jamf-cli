// Copyright 2026, Jamf Software LLC

package commands

import "io"

func renderDashboard(w io.Writer, data *DashboardData) error {
	_, err := w.Write([]byte("<html><body>placeholder</body></html>"))
	return err
}
