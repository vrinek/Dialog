#!/usr/bin/env bash
#
# Fallback PDF build script using pdfroff
# This is used when Chromium is not available
#
# Usage: ./build-pdf-fallback.sh [--version VERSION]
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
            echo "Usage: ./build-pdf-fallback.sh [--version VERSION]"
            exit 1
            ;;
    esac
done

# Configuration
if [[ -n "$VERSION" ]]; then
    OUTPUT_PDF="dialog-protocol-${VERSION}.pdf"
else
    OUTPUT_PDF="dialog-protocol-spec.pdf"
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}Note: Using pdfroff fallback (tables and links may not render perfectly)${NC}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

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

echo "Checking files..."
for file in "${MARKDOWN_FILES[@]}"; do
    if [[ -f "$file" ]]; then
        echo "  ✓ $file"
    else
        echo -e "  ${RED}✗ $file (not found)${NC}"
        exit 1
    fi
done

echo ""
echo "Building PDF: $OUTPUT_PDF"
if pandoc "${MARKDOWN_FILES[@]}" -t ms | pdfroff -ms - > "$OUTPUT_PDF"; then
    echo -e "${GREEN}✓ Successfully generated $OUTPUT_PDF${NC}"
    ls -lh "$OUTPUT_PDF"
else
    echo -e "${RED}✗ Failed to generate PDF${NC}"
    exit 1
fi
