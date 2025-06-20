package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jung-kurt/gofpdf"
)

type Candidate struct {
	Name          string   `json:"name"`
	Email         string   `json:"email"`
	MobileNo      string   `json:"mobile_no"`
	Skills        []string `json:"skills"`
	Experience    string   `json:"experience"`
	Qualification string   `json:"qualification"`
	ResumeURL     string   `json:"resume_url"`
}

func main() {
	// Setup logging
	setupLogging()

	router := gin.Default()
	router.POST("/api/process-candidates", processCandidates)
	router.GET("/health", healthCheck)

	log.Println("Server started at :8081")
	router.Run(":8081")
}

func setupLogging() {
	// Create logs directory if it doesn't exist
	logDir := "/var/log/ats-candidate-processor"
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		// Try to create in /var/log, if permission denied, use local logs directory
		if err := os.MkdirAll(logDir, 0755); err != nil {
			log.Printf("Cannot create log directory %s: %v, using ./logs instead", logDir, err)
			logDir = "./logs"
			os.MkdirAll(logDir, 0755)
		}
	}

	// Create log file with timestamp
	timestamp := time.Now().Format("2006-01-02")
	logFileName := fmt.Sprintf("ats-processor-%s.log", timestamp)
	logPath := filepath.Join(logDir, logFileName)

	// Open log file
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("Failed to open log file %s: %v, logging to stdout only", logPath, err)
		return
	}

	// Create multi-writer to write to both file and stdout
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Printf("Logging initialized. Log file: %s", logPath)
}

func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":    "healthy",
		"service":   "ats-candidate-processor",
		"version":   "1.0.0",
		"timestamp": time.Now().Unix(),
	})
}

func processCandidates(c *gin.Context) {
	var req struct {
		TenantName  string      `json:"tenant_name"`
		CompanyName string      `json:"company_name"`
		Candidates  []Candidate `json:"candidates"`
	}
	if err := c.BindJSON(&req); err != nil {
		log.Printf("Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	// Validate required fields
	if req.TenantName == "" || req.CompanyName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant_name and company_name are required"})
		return
	}

	if len(req.Candidates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "candidates list cannot be empty"})
		return
	}

	jobID := uuid.New().String()
	log.Printf("Starting job %s for tenant: %s, company: %s with %d candidates", jobID, req.TenantName, req.CompanyName, len(req.Candidates))

	baseDir := filepath.Join("/tmp/candidate-processor", jobID)
	factsheetDir := filepath.Join(baseDir, "factsheets")
	tempDir := filepath.Join(baseDir, "temp")

	// Create directories
	os.MkdirAll(factsheetDir, 0755)
	os.MkdirAll(tempDir, 0755)

	// Ensure cleanup happens (but not the zip file since we're returning its path)
	defer func() {
		log.Printf("Cleaning up temporary files for job %s", jobID)
		if err := os.RemoveAll(baseDir); err != nil {
			log.Printf("Error cleaning up directory %s: %v", baseDir, err)
		} else {
			log.Printf("Successfully cleaned up temporary files for job %s", jobID)
		}
	}()

	var wg sync.WaitGroup
	var mu sync.Mutex
	errors := []string{}
	successCount := 0

	for _, candidate := range req.Candidates {
		wg.Add(1)
		go func(cand Candidate) {
			defer wg.Done()
			log.Printf("Processing candidate: %s (%s)", cand.Name, cand.Email)

			if err := handleCandidate(cand, factsheetDir, tempDir); err != nil {
				mu.Lock()
				errors = append(errors, fmt.Sprintf("%s: %v", cand.Email, err))
				mu.Unlock()
				log.Printf("Error processing candidate %s: %v", cand.Email, err)
			} else {
				mu.Lock()
				successCount++
				mu.Unlock()
				log.Printf("Successfully processed candidate: %s", cand.Email)
			}
		}(candidate)
	}

	wg.Wait()

	log.Printf("Processing completed. Success: %d, Errors: %d", successCount, len(errors))

	// Create zip file with only factsheets
	// Sanitize tenant and company names for filename
	sanitizedTenant := sanitizeFilename(req.TenantName)
	sanitizedCompany := sanitizeFilename(req.CompanyName)
	zipFileName := fmt.Sprintf("%s_%s_factsheets_%s.zip", sanitizedTenant, sanitizedCompany, jobID)
	zipPath := filepath.Join("/tmp", zipFileName)

	if err := zipFolder(factsheetDir, zipPath); err != nil {
		log.Printf("Error creating zip file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to zip files"})
		return
	}

	log.Printf("Created zip file: %s", zipPath)

	// Prepare response
	response := gin.H{
		"job_id":                 jobID,
		"tenant_name":            req.TenantName,
		"company_name":           req.CompanyName,
		"zip_file_path":          zipPath,
		"zip_file_name":          zipFileName,
		"total_candidates":       len(req.Candidates),
		"processed_successfully": successCount,
		"errors_count":           len(errors),
	}

	if len(errors) > 0 {
		log.Printf("Job %s completed with errors for %s - %s: %v", jobID, req.TenantName, req.CompanyName, errors)
		response["errors"] = errors
		response["status"] = "completed_with_errors"
	} else {
		log.Printf("Job %s completed successfully for %s - %s", jobID, req.TenantName, req.CompanyName)
		response["status"] = "completed_successfully"
	}

	c.JSON(http.StatusOK, response)
}

// sanitizeFilename removes or replaces characters that are not safe for filenames
func sanitizeFilename(filename string) string {
	// Replace spaces with underscores
	filename = strings.ReplaceAll(filename, " ", "_")
	// Replace special characters with underscores
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")
	filename = strings.ReplaceAll(filename, ":", "_")
	filename = strings.ReplaceAll(filename, "*", "_")
	filename = strings.ReplaceAll(filename, "?", "_")
	filename = strings.ReplaceAll(filename, "\"", "_")
	filename = strings.ReplaceAll(filename, "<", "_")
	filename = strings.ReplaceAll(filename, ">", "_")
	filename = strings.ReplaceAll(filename, "|", "_")
	filename = strings.ReplaceAll(filename, "@", "_")
	filename = strings.ReplaceAll(filename, "#", "_")
	filename = strings.ReplaceAll(filename, "%", "_")
	filename = strings.ReplaceAll(filename, "&", "_")
	filename = strings.ReplaceAll(filename, "+", "_")
	filename = strings.ReplaceAll(filename, "=", "_")

	// Remove multiple consecutive underscores
	for strings.Contains(filename, "__") {
		filename = strings.ReplaceAll(filename, "__", "_")
	}

	// Trim underscores from start and end
	filename = strings.Trim(filename, "_")

	// Limit length to 50 characters
	if len(filename) > 50 {
		filename = filename[:50]
	}

	return filename
}

func handleCandidate(cand Candidate, factsheetDir, tempDir string) error {
	// Create candidate-specific temp directory
	candTempDir := filepath.Join(tempDir, strings.ReplaceAll(cand.Email, "@", "_"))
	os.MkdirAll(candTempDir, 0755)

	// Generate factsheet directly in factsheet directory
	factsheetPath := filepath.Join(factsheetDir, fmt.Sprintf("%s_factsheet.pdf", strings.ReplaceAll(cand.Email, "@", "_")))
	if err := generateFactsheetPDF(cand, factsheetPath); err != nil {
		return fmt.Errorf("failed to generate factsheet: %w", err)
	}

	// Download resume to temp directory
	resumeFile := filepath.Join(candTempDir, "resume")
	if err := downloadFile(cand.ResumeURL, resumeFile); err != nil {
		return fmt.Errorf("failed to download resume: %w", err)
	}

	// Convert resume to PDF in temp directory
	resumePDF := resumeFile + ".pdf"
	if strings.HasSuffix(strings.ToLower(cand.ResumeURL), ".pdf") {
		os.Rename(resumeFile, resumePDF)
	} else {
		if _, err := convertToPDF(resumeFile, candTempDir); err != nil {
			return fmt.Errorf("conversion failed: %w", err)
		}
	}

	// Merge PDFs and save final result as factsheet
	mergedPath := filepath.Join(candTempDir, "merged.pdf")
	if err := mergePDFs(factsheetPath, resumePDF, mergedPath); err != nil {
		return fmt.Errorf("failed to merge pdfs: %w", err)
	}

	// Replace the original factsheet with merged version
	if err := os.Rename(mergedPath, factsheetPath); err != nil {
		return fmt.Errorf("failed to move merged file: %w", err)
	}

	return nil
}

func generateFactsheetPDF(cand Candidate, outputPath string) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	// Title
	pdf.SetFont("Arial", "B", 18)
	pdf.SetFillColor(240, 240, 240)
	pdf.CellFormat(190, 12, "CANDIDATE FACTSHEET", "1", 1, "C", true, 0, "")
	pdf.Ln(8)

	// Table setup
	pdf.SetFont("Arial", "B", 12)
	pdf.SetFillColor(220, 220, 220)

	// Table rows
	tableData := [][]string{
		{"Name", cand.Name},
		{"Email", cand.Email},
		{"Mobile Number", cand.MobileNo},
		{"Qualification", cand.Qualification},
		{"Experience", cand.Experience},
		{"Skills", strings.Join(cand.Skills, ", ")},
	}

	// Column widths
	col1Width := 50.0
	col2Width := 140.0
	rowHeight := 10.0

	for i, row := range tableData {
		// Alternate row colors
		if i%2 == 0 {
			pdf.SetFillColor(250, 250, 250)
		} else {
			pdf.SetFillColor(240, 240, 240)
		}

		// Field name (bold)
		pdf.SetFont("Arial", "B", 11)
		pdf.CellFormat(col1Width, rowHeight, row[0], "1", 0, "L", true, 0, "")

		// Field value (normal)
		pdf.SetFont("Arial", "", 11)

		// Handle long text (especially skills) with MultiCell
		if row[0] == "Skills" && len(row[1]) > 50 {
			// Calculate required height for skills
			lines := pdf.SplitLines([]byte(row[1]), col2Width-4)
			cellHeight := float64(len(lines)) * 5.0
			if cellHeight < rowHeight {
				cellHeight = rowHeight
			}

			// Draw the cell border first
			pdf.CellFormat(col2Width, cellHeight, "", "1", 1, "L", true, 0, "")

			// Go back to write the text
			currentY := pdf.GetY() - cellHeight
			pdf.SetY(currentY + 1)
			pdf.SetX(pdf.GetX() + col1Width + 1)

			// Write multi-line text
			pdf.MultiCell(col2Width-2, 5, row[1], "", "L", false)

			// Move to next row position
			pdf.SetY(currentY + cellHeight)
		} else {
			pdf.CellFormat(col2Width, rowHeight, row[1], "1", 1, "L", true, 0, "")
		}
	}

	// Add footer
	pdf.Ln(10)
	pdf.SetFont("Arial", "I", 9)
	pdf.SetTextColor(128, 128, 128)
	pdf.Cell(190, 5, fmt.Sprintf("Generated on: %s", time.Now().Format("2006-01-02 15:04:05")))

	return pdf.OutputFileAndClose(outputPath)
}

func downloadFile(url, outputPath string) error {
	log.Printf("Downloading file from URL: %s", url)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	log.Printf("File downloaded successfully: %s", outputPath)
	return err
}

func convertToPDF(inputPath, outputDir string) (string, error) {
	log.Printf("Converting file to PDF: %s", inputPath)
	cmd := exec.Command("libreoffice", "--headless", "--convert-to", "pdf", "--outdir", outputDir, inputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, stderr.String())
	}

	outputFile := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)) + ".pdf"
	outputPath := filepath.Join(outputDir, outputFile)
	log.Printf("File converted to PDF: %s", outputPath)
	return outputPath, nil
}

func mergePDFs(pdf1, pdf2, outputPath string) error {
	log.Printf("Merging PDFs: %s + %s -> %s", pdf1, pdf2, outputPath)
	cmd := exec.Command("pdfunite", pdf1, pdf2, outputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pdfunite failed: %v - %s", err, stderr.String())
	}

	log.Printf("PDFs merged successfully: %s", outputPath)
	return nil
}

func zipFolder(sourceDir, zipPath string) error {
	log.Printf("Creating zip file from directory: %s -> %s", sourceDir, zipPath)
	zipfile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipfile.Close()

	archive := zip.NewWriter(zipfile)
	defer archive.Close()

	fileCount := 0
	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(sourceDir, path)
		zipEntry, err := archive.Create(relPath)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(zipEntry, file)
		if err == nil {
			fileCount++
		}
		return err
	})

	log.Printf("Zip file created with %d files: %s", fileCount, zipPath)
	return err
}
