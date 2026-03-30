#!/bin/bash
set -e

API_BASE_URL="${API_BASE_URL:-http://localhost:8080}"
TIMEOUT=30

echo "Starting integration tests..."

test_healthz() {
    echo "Testing /healthz..."
    response=$(curl -s -o /dev/null -w "%{http_code}" "$API_BASE_URL/healthz")
    if [ "$response" -eq 200 ]; then
        echo "✓ Health check passed"
    else
        echo "✗ Health check failed (HTTP $response)"
        exit 1
    fi
}

test_create_voting() {
    echo "Testing POST /votings..."
    
    payload='{
        "name": "Integration Test Voting",
        "status": "OPEN",
        "candidates": [
            {"candidateId": "c1", "name": "Candidate 1"},
            {"candidateId": "c2", "name": "Candidate 2"}
        ]
    }'
    
    response=$(curl -s -X POST "$API_BASE_URL/votings" \
        -H "Content-Type: application/json" \
        -d "$payload" \
        -w "\n%{http_code}")
    
    http_code=$(echo "$response" | tail -1)
    body=$(echo "$response" | head -n -1)
    
    if [ "$http_code" -eq 201 ]; then
        echo "✓ Voting created"
        voting_id=$(echo "$body" | grep -o '"votingId":"[^"]*"' | cut -d'"' -f4)
        echo "  Voting ID: $voting_id"
        echo "$voting_id"
    else
        echo "✗ Voting creation failed (HTTP $http_code)"
        echo "  Body: $body"
        exit 1
    fi
}

test_get_voting() {
    local voting_id="$1"
    echo "Testing GET /votings/{$voting_id}..."
    
    response=$(curl -s -w "\n%{http_code}" "$API_BASE_URL/votings/$voting_id")
    http_code=$(echo "$response" | tail -1)
    
    if [ "$http_code" -eq 200 ]; then
        echo "✓ Voting retrieved"
    else
        echo "✗ Voting retrieval failed (HTTP $http_code)"
        exit 1
    fi
}

test_list_votings() {
    echo "Testing GET /votings..."
    
    response=$(curl -s -w "\n%{http_code}" "$API_BASE_URL/votings")
    http_code=$(echo "$response" | tail -1)
    
    if [ "$http_code" -eq 200 ]; then
        echo "✓ Votings listed"
    else
        echo "✗ Votings listing failed (HTTP $http_code)"
        exit 1
    fi
}

test_create_vote() {
    local voting_id="$1"
    echo "Testing POST /votings/{$voting_id}/votes..."
    
    payload='{
        "candidateId": "c1",
        "clientContext": {
            "userAgent": "integration-test"
        }
    }'
    
    response=$(curl -s -X POST "$API_BASE_URL/votings/$voting_id/votes" \
        -H "Content-Type: application/json" \
        -d "$payload" \
        -w "\n%{http_code}")
    
    http_code=$(echo "$response" | tail -1)
    
    if [ "$http_code" -eq 202 ] || [ "$http_code" -eq 409 ]; then
        echo "✓ Vote submitted (HTTP $http_code)"
    else
        echo "✗ Vote submission failed (HTTP $http_code)"
        exit 1
    fi
}

test_get_results() {
    local voting_id="$1"
    echo "Testing GET /votings/{$voting_id}/results..."
    
    response=$(curl -s -w "\n%{http_code}" "$API_BASE_URL/votings/$voting_id/results")
    http_code=$(echo "$response" | tail -1)
    
    if [ "$http_code" -eq 200 ]; then
        echo "✓ Results retrieved"
    else
        echo "✗ Results retrieval failed (HTTP $http_code)"
        exit 1
    fi
}

test_update_voting() {
    local voting_id="$1"
    echo "Testing PATCH /votings/{$voting_id}..."
    
    payload='{
        "status": "CLOSED"
    }'
    
    response=$(curl -s -X PATCH "$API_BASE_URL/votings/$voting_id" \
        -H "Content-Type: application/json" \
        -d "$payload" \
        -w "\n%{http_code}")
    
    http_code=$(echo "$response" | tail -1)
    
    if [ "$http_code" -eq 200 ]; then
        echo "✓ Voting updated"
    else
        echo "✗ Voting update failed (HTTP $http_code)"
        exit 1
    fi
}

run_tests() {
    test_healthz
    
    voting_id=$(test_create_voting)
    
    test_get_voting "$voting_id"
    test_list_votings
    test_create_vote "$voting_id"
    test_get_results "$voting_id"
    test_update_voting "$voting_id"
    
    echo ""
    echo "All integration tests passed!"
}

if [ -z "$SKIP_SETUP" ]; then
    echo "Waiting for API to be ready..."
    for i in $(seq 1 $TIMEOUT); do
        if curl -s "$API_BASE_URL/healthz" > /dev/null 2>&1; then
            echo "API is ready"
            break
        fi
        if [ $i -eq $TIMEOUT ]; then
            echo "API failed to start within $TIMEOUT seconds"
            exit 1
        fi
        sleep 1
    done
fi

run_tests
