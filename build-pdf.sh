#!/usr/bin/env bash
#
# Build script to generate a combined PDF from Dialog protocol specification
# Uses Chromium headless mode for high-quality PDF generation with proper
# tables, links, and formatting.
#

set -e

# Configuration
OUTPUT_PDF="dialog-protocol-spec.pdf"
OUTPUT_HTML="dialog-protocol-spec.html"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if pandoc is installed
if ! command -v pandoc &> /dev/null; then
    echo -e "${RED}Error: pandoc is not installed${NC}"
    echo "Please install pandoc: https://pandoc.org/installing.html"
    exit 1
fi

# Check if chromium is installed
CHROME_CMD=""
if command -v chromium &> /dev/null; then
    CHROME_CMD="chromium"
elif command -v google-chrome &> /dev/null; then
    CHROME_CMD="google-chrome"
elif command -v google-chrome-stable &> /dev/null; then
    CHROME_CMD="google-chrome-stable"
else
    echo -e "${YELLOW}Warning: Chromium/Chrome not found. Falling back to pdfroff.${NC}"
    echo "For better quality (tables, links), install Chromium:"
    echo "  • Arch Linux: sudo pacman -S chromium"
    echo "  • Ubuntu/Debian: sudo apt-get install chromium-browser"
    echo ""
    
    # Fall back to pdfroff if available
    if command -v pdfroff &> /dev/null; then
        echo "Using pdfroff as fallback..."
        ./build-pdf-fallback.sh
        exit $?
    else
        echo -e "${RED}Error: Neither Chromium nor pdfroff available${NC}"
        exit 1
    fi
fi

echo "Using PDF engine: $CHROME_CMD (Chromium headless)"

# Get the project root (directory where this script is located)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Collect all markdown files in order
MARKDOWN_FILES=(
    "README.md"
    "spec/00-overview.md"
    "spec/01-data-model.md"
    "spec/02-block-format.md"
    "spec/03-encoding.md"
    "spec/04-cryptography.md"
    "spec/05-processing-model.md"
    "spec/06-meta-bonds.md"
)

# Verify all files exist
echo "Checking files..."
missing=0
for file in "${MARKDOWN_FILES[@]}"; do
    if [[ -f "$file" ]]; then
        echo "  ✓ $file"
    else
        echo -e "  ${RED}✗ $file (not found)${NC}"
        missing=1
    fi
done

if [[ $missing -eq 1 ]]; then
    echo -e "${RED}Error: Some files are missing${NC}"
    exit 1
fi

# Step 1: Create combined markdown with page breaks between files
echo ""
echo "Step 1: Combining markdown files with page breaks..."

TEMP_MD=$(mktemp)
trap "rm -f $TEMP_MD" EXIT

# Concatenate all files with page break markers before each
# This ensures TOC is on its own page(s) and each file starts fresh
for file in "${MARKDOWN_FILES[@]}"; do
    # Add page break before each file
    echo "" >> "$TEMP_MD"
    echo '<div style="page-break-before: always;"></div>' >> "$TEMP_MD"
    echo "" >> "$TEMP_MD"
    cat "$file" >> "$TEMP_MD"
done

echo -e "${GREEN}✓ Combined markdown with page breaks${NC}"

# Step 2: Generate combined HTML
echo ""
echo "Step 2: Building combined HTML..."

PANDOC_ARGS=(
    -o "$OUTPUT_HTML"
    -f markdown
    --standalone
    --toc
    --toc-depth=3
    --metadata title="Dialog Protocol Specification"
)

if pandoc "${PANDOC_ARGS[@]}" "$TEMP_MD"; then
    echo -e "${GREEN}✓ HTML generated: $OUTPUT_HTML${NC}"
else
    echo -e "${RED}✗ Failed to generate HTML${NC}"
    exit 1
fi

# Step 3: Convert HTML to PDF using Chromium
echo ""
echo "Step 3: Converting HTML to PDF using Chromium..."

# Create a temporary user data directory for chromium
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR $TEMP_MD" EXIT

# Use chromium to convert HTML to PDF
# --headless: Run without UI
# --run-all-compositor-stages-before-draw: Ensure everything is rendered
# --print-to-pdf: Output to PDF
if $CHROME_CMD \
    --headless \
    --disable-gpu \
    --run-all-compositor-stages-before-draw \
    --print-to-pdf="$OUTPUT_PDF" \
    --print-to-pdf-no-header \
    --virtual-time-budget=10000 \
    "file://$SCRIPT_DIR/$OUTPUT_HTML" 2>/dev/null; then
    
    echo -e "${GREEN}✓ Successfully generated $OUTPUT_PDF${NC}"
    ls -lh "$OUTPUT_PDF"
    file "$OUTPUT_PDF"
    echo ""
    echo "The PDF includes:"
    echo "  • Each markdown file starts on a new page"
    echo "  • Properly formatted headers (h1, h2, h3)"
    echo "  • Tables rendered correctly"
    echo "  • Working hyperlinks"
    echo "  • Table of contents"
else
    echo -e "${RED}✗ Failed to generate PDF with Chromium${NC}"
    exit 1
fi
