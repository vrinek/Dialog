#!/bin/bash
#
# Inject version from git tag into specification files
# Replaces <<VERSION>> placeholder with actual tag version
#

set -euo pipefail

# Detect version from GITHUB_REF or git tag
detect_version() {
    # Priority 1: GitHub Actions environment
    if [ -n "${GITHUB_REF:-}" ]; then
        if [[ "$GITHUB_REF" == refs/tags/v* ]]; then
            echo "${GITHUB_REF#refs/tags/}"
            return 0
        fi
        echo "Error: GITHUB_REF does not point to a version tag: $GITHUB_REF" >&2
        return 1
    fi
    
    # Priority 2: Local git tags
    if git describe --tags --exact-match HEAD 2>/dev/null | grep -q '^v'; then
        git describe --tags --exact-match HEAD
        return 0
    fi
    
    echo "Error: No version tag available" >&2
    echo "This script requires either:" >&2
    echo "  - GITHUB_REF=refs/tags/v*.*.* (in CI)" >&2
    echo "  - Current HEAD to be at a v*.*.* tag (locally)" >&2
    return 1
}

# Update version in a single file
update_file() {
    local file="$1"
    local version="$2"
    
    if [ ! -f "$file" ]; then
        echo "Error: File not found: $file" >&2
        return 1
    fi
    
    # Check if placeholder exists
    if ! grep -q "<<VERSION>>" "$file"; then
        echo "Error: Placeholder <<VERSION>> not found in $file" >&2
        return 1
    fi
    
    # Replace placeholder with actual version
    sed -i.bak "s|<<VERSION>>|$version|g" "$file"
    rm -f "$file.bak"
    
    echo "  ✓ Updated $file"
}

main() {
    local version
    version=$(detect_version) || exit 1
    
    echo "Injecting version $version into spec files..."
    
    local files=(
        "spec/00-overview.md"
        "spec/01-data-model.md"
        "spec/02-block-format.md"
        "spec/03-encoding.md"
        "spec/04-cryptography.md"
        "spec/05-processing-model.md"
        "spec/06-meta-bonds.md"
    )
    
    for file in "${files[@]}"; do
        update_file "$file" "$version"
    done
    
    echo "✓ Version injection complete"
}

main "$@"
