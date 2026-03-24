#!/bin/bash
# Hydra Implementation Monitor
# Tracks: task completion, cass/llm-tldr usage, pi skill usage

echo "=== Hydra Implementation Monitor ==="
echo "Started: $(date)"
echo ""

# Task Completion Status
echo "📊 TASK COMPLETION STATUS"
echo "-------------------------"
total=$(br count 2>/dev/null | grep "Total" | awk '{print $2}')
closed=$(br count --status closed 2>/dev/null | grep "Total" | awk '{print $2}')
in_progress=$(br count --status in_progress 2>/dev/null | grep "Total" | awk '{print $2}')
ready=$(br count --status open 2>/dev/null | grep "Total" | awk '{print $2}')

if [ -n "$total" ]; then
    closed=${closed:-0}
    in_progress=${in_progress:-0}
    ready=${ready:-0}
    
    completed_percent=$((closed * 100 / total))
    
    echo "Total Tasks: $total"
    echo "Completed:   $closed ($completed_percent%)"
    echo "In Progress: $in_progress"
    echo "Ready:       $ready"
    echo ""
fi

# Recent completions (last hour)
echo "✅ RECENT COMPLETIONS (last hour)"
echo "----------------------------------"
br list --status closed 2>/dev/null | grep "$(date '+%Y-%m-%d')" | tail -5 || echo "No recent completions"
echo ""

# Currently in progress
echo "🔄 CURRENTLY IN PROGRESS"
echo "------------------------"
br list --status in_progress 2>/dev/null | head -10
echo ""

# cass-memory usage
echo "💾 CASS-MEMORY USAGE"
echo "--------------------"
ps aux | grep -E "(cass-memory|cass_memory)" | grep -v grep | wc -l | xargs -I {} echo "Active processes: {}"
if [ -f /tmp/cass-memory.log ]; then
    echo "Recent activity (last 10 lines):"
    tail -10 /tmp/cass-memory.log 2>/dev/null | grep -E "(ADD|GET|ERROR)" || echo "No recent cass-memory activity"
fi
echo ""

# llm-tldr usage  
echo "🔍 LLM-TLDR USAGE"
echo "-----------------"
ps aux | grep "llm-tldr" | grep -v grep | wc -l | xargs -I {} echo "Active processes: {}"
if [ -f /tmp/llm-tldr.log ]; then
    echo "Recent activity (last 10 lines):"
    tail -10 /tmp/llm-tldr.log 2>/dev/null | grep -E "(index|query|ERROR)" || echo "No recent llm-tldr activity"
fi
echo ""

# PI Skill Usage
echo "🤖 PI SKILL USAGE"
echo "-----------------"
echo "Recent skill invocations (from logs):"
if [ -f /tmp/hydra.log ]; then
    tail -50 /tmp/hydra.log 2>/dev/null | grep -E "(skill|prompt)" | tail -10 || echo "No skill activity in logs"
fi
echo ""

# Worker activity
echo "👷 WORKER ACTIVITY"
echo "------------------"
worker_count=$(ps aux | grep "hydra-worker\|worker " | grep -v grep | grep -v postgres | wc -l)
echo "Active workers: $worker_count"
ls -1 /Users/buddhi/source-code/personal/hydra/hydra-logs/workers/ 2>/dev/null | wc -l | xargs -I {} echo "Worker logs: {}"
echo ""

echo "=== Monitor completed: $(date) ==="
