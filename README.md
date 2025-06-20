# ATS Candidate Processor

A high-performance Go-based microservice designed for Applicant Tracking Systems (ATS) to process candidate data and generate professional factsheets with resume attachments.

## Overview

This service processes candidate information submitted through ATS platforms, downloads their resumes, generates professional factsheets, merges them with resumes, and packages everything into a downloadable ZIP file for HR teams and recruiters.

## Features

- **Batch Processing**: Handle multiple candidates simultaneously with concurrent processing
- **Professional Factsheets**: Generate well-formatted PDF factsheets with candidate information in table format
- **Resume Integration**: Download and convert various resume formats (PDF, DOC, DOCX) to PDF
- **Document Merging**: Combine factsheets with resumes into single PDF documents
- **Comprehensive Logging**: Detailed logging for audit trails and debugging
- **Error Handling**: Robust error handling with detailed error reporting
- **Clean Architecture**: Automatic cleanup of temporary files while preserving final outputs

## Prerequisites

### System Dependencies
```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install libreoffice poppler-utils

# CentOS/RHEL
sudo yum install libreoffice poppler-utils

# macOS
brew install libreoffice poppler
```

### Go Dependencies
```bash
go mod init ats-candidate-processor
go get github.com/gin-gonic/gin
go get github.com/google/uuid
go get github.com/jung-kurt/gofpdf
```

## Installation

1. **Clone the repository**
```bash
git clone <repository-url>
cd ats-candidate-processor
```

2. **Install dependencies**
```bash
go mod tidy
```

3. **Build the application**
```bash
go build -o candidate-processor main.go
```

4. **Run the service**
```bash
./candidate-processor
```

The service will start on port `8081` by default.

## API Documentation

### Process Candidates Endpoint

**Endpoint**: `POST /api/process-candidates`

**Request Body**:
```json
{
  "candidates": [
    {
      "name": "John Doe",
      "email": "john.doe@example.com",
      "mobile_no": "+1-234-567-8900",
      "skills": ["JavaScript", "React", "Node.js", "MongoDB"],
      "experience": "5 years in full-stack development",
      "qualification": "Bachelor's in Computer Science",
      "resume_url": "https://example.com/resumes/john-doe-resume.pdf"
    }
  ]
}
```

**Response**:
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "zip_file_path": "/tmp/factsheets_550e8400-e29b-41d4-a716-446655440000.zip",
  "total_candidates": 1,
  "processed_successfully": 1,
  "errors_count": 0,
  "status": "completed_successfully"
}
```

**Error Response**:
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "zip_file_path": "/tmp/factsheets_550e8400-e29b-41d4-a716-446655440000.zip",
  "total_candidates": 2,
  "processed_successfully": 1,
  "errors_count": 1,
  "status": "completed_with_errors",
  "errors": ["jane.doe@example.com: failed to download resume: HTTP 404"]
}
```

## Usage Examples

### cURL Example
```bash
curl -X POST http://localhost:8081/api/process-candidates \
  -H "Content-Type: application/json" \
  -d '{
    "candidates": [
      {
        "name": "Alice Johnson",
        "email": "alice.johnson@example.com",
        "mobile_no": "+1-555-0123",
        "skills": ["Python", "Django", "PostgreSQL", "AWS"],
        "experience": "3 years in backend development",
        "qualification": "Master's in Software Engineering",
        "resume_url": "https://example.com/resumes/alice-resume.pdf"
      }
    ]
  }'
```

### JavaScript/Node.js Example
```javascript
const axios = require('axios');

const processCandidates = async (candidates) => {
  try {
    const response = await axios.post('http://localhost:8081/api/process-candidates', {
      candidates: candidates
    });
    
    console.log('Job ID:', response.data.job_id);
    console.log('ZIP file path:', response.data.zip_file_path);
    console.log('Status:', response.data.status);
    
    return response.data;
  } catch (error) {
    console.error('Error processing candidates:', error.response?.data || error.message);
  }
};

// Usage
const candidates = [
  {
    name: "Bob Smith",
    email: "bob.smith@example.com",
    mobile_no: "+1-555-0456",
    skills: ["Java", "Spring Boot", "MySQL", "Docker"],
    experience: "4 years in enterprise software development",
    qualification: "Bachelor's in Information Technology",
    resume_url: "https://example.com/resumes/bob-resume.docx"
  }
];

processCandidates(candidates);
```

### Python Example
```python
import requests
import json

def process_candidates(candidates):
    url = "http://localhost:8081/api/process-candidates"
    payload = {"candidates": candidates}
    
    try:
        response = requests.post(url, json=payload)
        response.raise_for_status()
        
        result = response.json()
        print(f"Job ID: {result['job_id']}")
        print(f"ZIP file path: {result['zip_file_path']}")
        print(f"Status: {result['status']}")
        
        return result
    except requests.exceptions.RequestException as e:
        print(f"Error processing candidates: {e}")
        return None

# Usage
candidates = [
    {
        "name": "Carol Williams",
        "email": "carol.williams@example.com",
        "mobile_no": "+1-555-0789",
        "skills": ["C#", ".NET Core", "SQL Server", "Azure"],
        "experience": "6 years in .NET development",
        "qualification": "Bachelor's in Computer Engineering",
        "resume_url": "https://example.com/resumes/carol-resume.pdf"
    }
]

process_candidates(candidates)
```

## Output Structure

The generated ZIP file contains:
```
factsheets_<job-id>.zip
├── candidate1_email_com_factsheet.pdf
├── candidate2_email_com_factsheet.pdf
└── candidate3_email_com_factsheet.pdf
```

Each factsheet PDF contains:
1. **Candidate Information Table**: Professional table format with candidate details
2. **Resume Pages**: Original resume converted to PDF and appended

## Configuration

### Environment Variables
```bash
# Server port (default: 8081)
export PORT=8081

# Temporary directory (default: /tmp/candidate-processor)
export TEMP_DIR=/tmp/candidate-processor

# Download timeout (default: 60 seconds)
export DOWNLOAD_TIMEOUT=60
```

### Supported Resume Formats
- PDF (`.pdf`)
- Microsoft Word (`.doc`, `.docx`)
- OpenDocument Text (`.odt`)
- Rich Text Format (`.rtf`)
- Plain Text (`.txt`)

## Integration with ATS Systems

### Webhook Integration
For real-time processing, configure your ATS to send webhook requests:

```javascript
// ATS Webhook Handler Example
app.post('/ats-webhook', async (req, res) => {
  const { candidates } = req.body;
  
  // Process candidates
  const result = await processCandidates(candidates);
  
  if (result.status === 'completed_successfully') {
    // Notify HR team or update ATS database
    await notifyHRTeam(result.zip_file_path);
  }
  
  res.json({ status: 'processed', job_id: result.job_id });
});
```

### Database Integration
Store job results in your ATS database:

```sql
CREATE TABLE candidate_processing_jobs (
    id UUID PRIMARY KEY,
    job_id VARCHAR(255) UNIQUE NOT NULL,
    zip_file_path VARCHAR(500) NOT NULL,
    total_candidates INTEGER NOT NULL,
    processed_successfully INTEGER NOT NULL,
    errors_count INTEGER NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    errors_detail TEXT
);
```

## Monitoring and Logging

### Log Levels
- **INFO**: Job start/completion, successful operations
- **ERROR**: Processing failures, system errors
- **DEBUG**: Detailed operation tracking

### Log Format
```
2025-06-20 10:30:15 [INFO] Starting job 550e8400-e29b-41d4-a716-446655440000 with 5 candidates
2025-06-20 10:30:16 [INFO] Processing candidate: John Doe (john.doe@example.com)
2025-06-20 10:30:17 [INFO] Downloading file from URL: https://example.com/resume.pdf
2025-06-20 10:30:18 [INFO] File downloaded successfully: /tmp/candidate-processor/temp/resume
2025-06-20 10:30:19 [INFO] Successfully processed candidate: john.doe@example.com
```

### Health Check Endpoint
Add this endpoint for monitoring:

```go
router.GET("/health", func(c *gin.Context) {
    c.JSON(200, gin.H{
        "status": "healthy",
        "service": "ats-candidate-processor",
        "version": "1.0.0",
        "timestamp": time.Now().Unix(),
    })
})
```

## Performance Considerations

- **Concurrent Processing**: Processes multiple candidates simultaneously using goroutines
- **Memory Management**: Automatic cleanup of temporary files
- **Timeout Handling**: 60-second timeout for resume downloads
- **Error Isolation**: Individual candidate failures don't affect batch processing

### Recommended Limits
- **Max candidates per request**: 50
- **Max resume file size**: 10MB
- **Concurrent jobs**: Limited by system resources

## Troubleshooting

### Common Issues

1. **LibreOffice not found**
   ```bash
   sudo apt-get install libreoffice
   # or
   brew install libreoffice
   ```

2. **pdfunite command not found**
   ```bash
   sudo apt-get install poppler-utils
   # or
   brew install poppler
   ```

3. **Permission denied on /tmp**
   ```bash
   sudo chmod 755 /tmp
   # or set custom temp directory
   export TEMP_DIR=/home/user/temp
   ```

4. **Resume download failures**
   - Check resume URL accessibility
   - Verify network connectivity
   - Check file size limits

### Debug Mode
Run with debug logging:
```bash
GIN_MODE=debug ./candidate-processor
```

## Security Considerations

- **Input Validation**: Validate all candidate data before processing
- **URL Sanitization**: Sanitize resume URLs to prevent SSRF attacks
- **File Type Validation**: Verify downloaded file types
- **Temporary File Cleanup**: Automatic cleanup prevents data leakage
- **Rate Limiting**: Implement rate limiting for production use

## Docker Deployment

```dockerfile
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache libreoffice poppler-utils

WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o candidate-processor main.go

FROM alpine:latest
RUN apk add --no-cache libreoffice poppler-utils ca-certificates
WORKDIR /root/

COPY --from=builder /app/candidate-processor .

EXPOSE 8081
CMD ["./candidate-processor"]
```

```bash
docker build -t ats-candidate-processor .
docker run -p 8081:8081 ats-candidate-processor
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/new-feature`)
3. Commit your changes (`git commit -am 'Add new feature'`)
4. Push to the branch (`git push origin feature/new-feature`)
5. Create a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For support and questions:
- Create an issue in the GitHub repository
- Contact the development team
- Check the troubleshooting section above

---

**Version**: 1.0.0  
**Last Updated**: June 2025