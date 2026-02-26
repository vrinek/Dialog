#!/usr/bin/env bash
#
# Build script to generate a combined HTML file from Dialog protocol specification
#
# Usage: ./build-html.sh [--version VERSION]
#   --version VERSION   Add version suffix to filename (e.g., v0.2.0)
#

set -e

# Parse arguments
VERSION=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --version)
            VERSION="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: ./build-html.sh [--version VERSION]"
            exit 1
            ;;
    esac
done

# Configuration
if [[ -n "$VERSION" ]]; then
    OUTPUT_HTML="dialog-protocol-${VERSION}.html"
else
    OUTPUT_HTML="dialog-protocol-spec.html"
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Check if pandoc is installed
if ! command -v pandoc &> /dev/null; then
    echo -e "${RED}Error: pandoc is not installed${NC}"
    echo "Please install pandoc: https://pandoc.org/installing.html"
    exit 1
fi

# Get the project root (directory where this script is located)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Collect all markdown files in order
# README.md first, then spec files in numbered order
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

# Build pandoc command
echo ""
echo "Building HTML: $OUTPUT_HTML"

PANDOC_ARGS=(
    -o "$OUTPUT_HTML"
    -f markdown
    --standalone
    --toc
    --toc-depth=3
    --metadata title="Dialog Protocol Specification"
    -c style.css
)

# Check for custom CSS
if [[ -f "style.css" ]]; then
    echo "Using custom style.css"
fi

# Run pandoc
echo "Running pandoc..."
if pandoc "${PANDOC_ARGS[@]}" "${MARKDOWN_FILES[@]}"; then
    echo -e "${GREEN}✓ Successfully generated $OUTPUT_HTML${NC}"
    ls -lh "$OUTPUT_HTML"
    echo ""
    echo "To view: open $OUTPUT_HTML"
else
    echo -e "${RED}✗ Failed to generate HTML${NC}"
    exit 1
fi
