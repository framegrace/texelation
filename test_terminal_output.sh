#!/bin/bash
# Simple test to verify terminal output with the editor card

echo "=== Testing terminal output with long line editor ==="
echo ""

# Create a test script
cat > /tmp/test_term_output.sh <<'EOF'
#!/bin/sh
echo "Test line 1"
echo "Test line 2"
echo "Test line 3"
echo "SUCCESS"
EOF
chmod +x /tmp/test_term_output.sh

# Run the test with the unit test instead
echo "Running unit test..."
go test -v ./apps/texelterm/longeditor -run TestEditorCardDoesNotBreakTerminalOutput 2>&1 | grep -E "(PASS|FAIL|Buffer|found)"

rm -f /tmp/test_term_output.sh
