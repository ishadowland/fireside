@rem
@rem Minimal gradlew.bat stub. See ../gradlew for the bootstrapping instructions.
@rem

@echo off
setlocal

if not exist "gradle\wrapper\gradle-wrapper.jar" (
    echo error: gradle-wrapper.jar is missing. 1>&2
    echo        Run "gradle wrapper --gradle-version 8.9 --distribution-type bin" once, 1>&2
    echo        then "git add gradlew gradlew.bat gradle\wrapper\". 1>&2
    exit /b 1
)

if defined JAVA_HOME (
    set "JAVA=%JAVA_HOME%\bin\java.exe"
) else (
    for /f "delims=" %%i in ('where java') do set "JAVA=%%i"
)

"%JAVA%" -classpath "gradle\wrapper\gradle-wrapper.jar" org.gradle.wrapper.GradleWrapperMain %*

endlocal