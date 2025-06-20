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

const PORT string = "8081"

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
	router := gin.Default()
	router.POST("/api/process-candidates", processCandidates)

	log.Printf("Server started at :%s", PORT)
	router.Run(PORT)
}

// processCandidates is the main handler function for the /api/process-candidates endpoint. It takes a JSON
// payload with a list of candidates and processes them in parallel, generating a factsheet for each candidate
// and storing them in a temp directory. It then zips up the factsheets and returns a JSON response with a link
// to the zip file, the job ID, and some statistics about the processing.
//
// The function also logs the progress and any errors to the console.
func processCandidates(c *gin.Context) {
	var req struct {
		Candidates []Candidate `json:"candidates"`
	}
	if err := c.BindJSON(&req); err != nil {
		log.Printf("Error binding JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	jobID := uuid.New().String()
	log.Printf("Starting job %s with %d candidates", jobID, len(req.Candidates))

	baseDir := filepath.Join("/tmp/candidate-processor", jobID)
	factsheetDir := filepath.Join(baseDir, "factsheets")
	tempDir := filepath.Join(baseDir, "temp")

	os.MkdirAll(factsheetDir, 0755)
	os.MkdirAll(tempDir, 0755)

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

	zipPath := filepath.Join("/tmp", fmt.Sprintf("factsheets_%s.zip", jobID))
	if err := zipFolder(factsheetDir, zipPath); err != nil {
		log.Printf("Error creating zip file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to zip files"})
		return
	}

	log.Printf("Created zip file: %s", zipPath)

	response := gin.H{
		"job_id":                 jobID,
		"zip_file_path":          zipPath,
		"total_candidates":       len(req.Candidates),
		"processed_successfully": successCount,
		"errors_count":           len(errors),
	}

	if len(errors) > 0 {
		log.Printf("Job %s completed with errors: %v", jobID, errors)
		response["errors"] = errors
		response["status"] = "completed_with_errors"
	} else {
		log.Printf("Job %s completed successfully", jobID)
		response["status"] = "completed_successfully"
	}

	c.JSON(http.StatusOK, response)
}

// handleCandidate takes a Candidate and the paths to a factsheet directory and a temp directory, and processes the candidate by:
// - generating a factsheet PDF
// - downloading the resume
// - converting the resume to PDF if needed
// - merging the factsheet and resume PDFs
// - renaming the merged PDF to the factsheet path
//
// It returns an error if any of the above steps fail.
func handleCandidate(cand Candidate, factsheetDir, tempDir string) error {

	candTempDir := filepath.Join(tempDir, strings.ReplaceAll(cand.Email, "@", "_"))
	os.MkdirAll(candTempDir, 0755)

	factsheetPath := filepath.Join(factsheetDir, fmt.Sprintf("%s_factsheet.pdf", strings.ReplaceAll(cand.Email, "@", "_")))
	if err := generateFactsheetPDF(cand, factsheetPath); err != nil {
		return fmt.Errorf("failed to generate factsheet: %w", err)
	}

	resumeFile := filepath.Join(candTempDir, "resume")
	if err := downloadFile(cand.ResumeURL, resumeFile); err != nil {
		return fmt.Errorf("failed to download resume: %w", err)
	}

	resumePDF := resumeFile + ".pdf"
	if strings.HasSuffix(strings.ToLower(cand.ResumeURL), ".pdf") {
		os.Rename(resumeFile, resumePDF)
	} else {
		if _, err := convertToPDF(resumeFile, candTempDir); err != nil {
			return fmt.Errorf("conversion failed: %w", err)
		}
	}

	mergedPath := filepath.Join(candTempDir, "merged.pdf")
	if err := mergePDFs(factsheetPath, resumePDF, mergedPath); err != nil {
		return fmt.Errorf("failed to merge pdfs: %w", err)
	}

	if err := os.Rename(mergedPath, factsheetPath); err != nil {
		return fmt.Errorf("failed to move merged file: %w", err)
	}

	return nil
}

// generateFactsheetPDF generates a PDF factsheet for the given candidate and saves it to the given file path.
//
// The factsheet will contain the candidate's name, email, mobile number, qualification, experience, and skills.
// The information will be rendered in a table format with two columns. The left column will contain the field
// name and the right column will contain the field value. The skills field will be rendered as a multi-line text
// box if the skills string is longer than 50 characters.
//
// The PDF will have a header with the title "CANDIDATE FACTSHEET" and a footer with the current date and time.
//
// The function will return an error if the PDF could not be generated or saved.
func generateFactsheetPDF(cand Candidate, outputPath string) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	pdf.SetFont("Arial", "B", 18)
	pdf.SetFillColor(240, 240, 240)
	pdf.CellFormat(190, 12, "CANDIDATE FACTSHEET", "1", 1, "C", true, 0, "")
	pdf.Ln(8)

	pdf.SetFont("Arial", "B", 12)
	pdf.SetFillColor(220, 220, 220)

	tableData := [][]string{
		{"Name", cand.Name},
		{"Email", cand.Email},
		{"Mobile Number", cand.MobileNo},
		{"Qualification", cand.Qualification},
		{"Experience", cand.Experience},
		{"Skills", strings.Join(cand.Skills, ", ")},
	}

	col1Width := 50.0
	col2Width := 140.0
	rowHeight := 10.0

	for i, row := range tableData {

		if i%2 == 0 {
			pdf.SetFillColor(250, 250, 250)
		} else {
			pdf.SetFillColor(240, 240, 240)
		}

		pdf.SetFont("Arial", "B", 11)
		pdf.CellFormat(col1Width, rowHeight, row[0], "1", 0, "L", true, 0, "")

		pdf.SetFont("Arial", "", 11)

		if row[0] == "Skills" && len(row[1]) > 50 {

			lines := pdf.SplitLines([]byte(row[1]), col2Width-4)
			cellHeight := float64(len(lines)) * 5.0
			if cellHeight < rowHeight {
				cellHeight = rowHeight
			}

			pdf.CellFormat(col2Width, cellHeight, "", "1", 1, "L", true, 0, "")

			currentY := pdf.GetY() - cellHeight
			pdf.SetY(currentY + 1)
			pdf.SetX(pdf.GetX() + col1Width + 1)

			pdf.MultiCell(col2Width-2, 5, row[1], "", "L", false)

			pdf.SetY(currentY + cellHeight)
		} else {
			pdf.CellFormat(col2Width, rowHeight, row[1], "1", 1, "L", true, 0, "")
		}
	}

	pdf.Ln(10)
	pdf.SetFont("Arial", "I", 9)
	pdf.SetTextColor(128, 128, 128)
	pdf.Cell(190, 5, fmt.Sprintf("Generated on: %s", time.Now().Format("2006-01-02 15:04:05")))

	return pdf.OutputFileAndClose(outputPath)
}

// downloadFile downloads a file from a URL and saves it to a file at the given output path.
// It logs the progress and any errors to the console.
// It returns an error if the download fails or if writing the file fails.
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

// convertToPDF converts a file to a PDF using LibreOffice.
//
// The function takes the path to the file to be converted and the directory where the
// PDF should be saved. It logs the progress and any errors to the console.
// It returns the path to the converted PDF file and an error if the conversion fails.
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

// mergePDFs merges two PDF files together and saves the result to the given output path.
//
// The function takes the paths to the two PDF files and the desired output path. It logs the progress and any errors to the console.
// It returns an error if the merging fails.
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

// zipFolder takes a source directory and a zip file path, and creates a zip file with all the files in the source directory.
// It logs the progress and any errors to the console.
// It returns an error if the zip file creation fails.

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
