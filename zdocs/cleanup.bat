@echo off
setlocal enabledelayedexpansion
color 0A
title Task Cleanup - FIXED with Query Parameters

echo ================================================
echo 🧹 TASK CLEANUP - CORRECT QUERY PARAMETER FORMAT
echo ================================================

echo 📋 Current tasks:
curl -s -X GET "http://localhost/tasks" > current.tmp
type current.tmp
echo.

echo 🗑️ Deleting tasks using QUERY PARAMETERS...

echo Deleting Task ID: 2
curl -X DELETE "http://localhost/tasks?id=2" > delete_2.tmp
type delete_2.tmp | find "error\|Error\|fail" >nul && (
    echo ❌ Task 2: FAILED
    type delete_2.tmp
) || (
    echo ✅ Task 2: DELETED
)

echo.
echo Deleting Task ID: 3
curl -X DELETE "http://localhost/tasks?id=3" > delete_3.tmp
type delete_3.tmp | find "error\|Error\|fail" >nul && (
    echo ❌ Task 3: FAILED
    type delete_3.tmp
) || (
    echo ✅ Task 3: DELETED
)

echo.
echo Deleting Task ID: 4
curl -X DELETE "http://localhost/tasks?id=4" > delete_4.tmp
type delete_4.tmp | find "error\|Error\|fail" >nul && (
    echo ❌ Task 4: FAILED
    type delete_4.tmp
) || (
    echo ✅ Task 4: DELETED
)

echo.
echo Deleting Task ID: 5
curl -X DELETE "http://localhost/tasks?id=5" > delete_5.tmp
type delete_5.tmp | find "error\|Error\|fail" >nul && (
    echo ❌ Task 5: FAILED
    type delete_5.tmp
) || (
    echo ✅ Task 5: DELETED
)

echo.
echo 🧪 Final verification:
curl -s -X GET "http://localhost/tasks" > final.tmp

type final.tmp | find "id" >nul && (
    echo ⚠️ Some tasks still remain:
    type final.tmp
) || (
    echo ✅ ALL TASKS SUCCESSFULLY DELETED!
    echo []
)

del /q *.tmp 2>nul
echo.
echo ================================================
echo ✅ CLEANUP COMPLETED!
echo ================================================
pause
