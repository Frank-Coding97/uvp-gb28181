#include <windows.h>
#include <iostream>
#include <string>
#include <vector>

struct Evidence {
    bool argumentsValid = false, ownerStarted = false, ownerInitialEvent = false;
    bool childCreateEvent = false, childPathMatched = false, childHandleDuplicated = false;
    bool ownerTerminationAttempted = false, ownerTerminationSucceeded = false;
    bool ownerExitObserved = false, childExitObserved = false, childWaitSignaled = false;
    bool allEventsContinued = true;
    int ownerExitCode = -1, childExitCode = -1;
};
static bool isAbsolute(const std::wstring& p) {
    return (p.size() >= 3 && p[1] == L':' && (p[2] == L'\\' || p[2] == L'/')) ||
           (p.size() >= 2 && p[0] == L'\\' && p[1] == L'\\');
}
static bool fullPath(const std::wstring& input, std::wstring& output) {
    wchar_t buffer[32768];
    DWORD n = GetFullPathNameW(input.c_str(), static_cast<DWORD>(_countof(buffer)),
                               buffer, nullptr);
    if (n == 0 || n >= _countof(buffer)) return false;
    output.assign(buffer, n);
    return true;
}
static bool imagePath(HANDLE process, std::wstring& output) {
    wchar_t buffer[32768];
    DWORD n = static_cast<DWORD>(_countof(buffer));
    if (!QueryFullProcessImageNameW(process, 0, buffer, &n)) return false;
    output.assign(buffer, n);
    return true;
}
static bool samePath(const std::wstring& a, const std::wstring& b) {
    std::wstring af, bf;
    if (!fullPath(a, af) || !fullPath(b, bf)) return false;
    return CompareStringOrdinal(af.data(), static_cast<int>(af.size()), bf.data(),
                                static_cast<int>(bf.size()), TRUE) == CSTR_EQUAL;
}
static int report(const Evidence& e, bool success) {
    auto b = [](const char* name, bool value, bool comma = true) {
        std::cout << '"' << name << "\":" << (value ? "true" : "false")
                  << (comma ? "," : "");
    };
    std::cout << '{';
    b("arguments_valid", e.argumentsValid);
    b("owner_started", e.ownerStarted);
    b("owner_initial_event", e.ownerInitialEvent);
    b("child_create_event", e.childCreateEvent);
    b("child_path_matched", e.childPathMatched);
    b("child_handle_duplicated", e.childHandleDuplicated);
    b("owner_termination_attempted", e.ownerTerminationAttempted);
    b("owner_termination_succeeded", e.ownerTerminationSucceeded);
    b("owner_exit_observed", e.ownerExitObserved);
    b("child_exit_observed", e.childExitObserved);
    b("child_wait_signaled", e.childWaitSignaled);
    b("all_events_continued", e.allEventsContinued);
    std::cout << "\"owner_exit_code\":" << e.ownerExitCode
              << ",\"child_exit_code\":" << e.childExitCode << ",";
    b("success", success);
    std::cout << "\"exit_code\":" << (success ? 0 : 1) << "}\n";
    return success ? 0 : 1;
}
int wmain(int argc, wchar_t** argv) {
    Evidence e;
    if (argc != 3) return report(e, false);
    std::wstring ownerPath, redisPath;
    if (!isAbsolute(argv[1]) || !isAbsolute(argv[2]) ||
        !fullPath(argv[1], ownerPath) || !fullPath(argv[2], redisPath)) {
        return report(e, false);
    }
    e.argumentsValid = true;
    std::wstring command = L"\"" + ownerPath + L"\" --no-browser";
    std::vector<wchar_t> commandBuffer(command.begin(), command.end());
    commandBuffer.push_back(L'\0');
    std::wstring directory = ownerPath.substr(0, ownerPath.find_last_of(L"\\/"));
    STARTUPINFOW startup{};
    startup.cb = sizeof(startup);
    PROCESS_INFORMATION pi{};
    if (!CreateProcessW(ownerPath.c_str(), commandBuffer.data(), nullptr, nullptr, FALSE,
                        DEBUG_PROCESS, nullptr, directory.c_str(), &startup, &pi)) {
        return report(e, false);
    }
    e.ownerStarted = true;
    HANDLE childSync = nullptr;
    DWORD childPid = 0;
    bool firstEvent = true, failure = false, terminationAttempted = false;
    ULONGLONG debugDeadline = GetTickCount64() + 60000, drainDeadline = 0;
    auto beginDrain = [&] {
        if (drainDeadline == 0) drainDeadline = GetTickCount64() + 5000;
    };
    auto terminateOwner = [&] {
        if (terminationAttempted || e.ownerExitObserved) return;
        terminationAttempted = true;
        e.ownerTerminationAttempted = true;
        e.ownerTerminationSucceeded = TerminateProcess(pi.hProcess, 97) != FALSE;
        if (!e.ownerTerminationSucceeded) {
            failure = true;
            beginDrain();
        }
    };
    auto fail = [&] {
        failure = true;
        terminateOwner();
        beginDrain();
    };
    while (!e.ownerExitObserved || (childPid != 0 && !e.childExitObserved)) {
        ULONGLONG now = GetTickCount64();
        ULONGLONG limit = failure ? drainDeadline : debugDeadline;
        if (limit == 0 || now >= limit) {
            if (!failure) { fail(); continue; }
            break;
        }
        ULONGLONG remaining = limit - now;
        DWORD waitMs = remaining > 1000 ? 1000 : static_cast<DWORD>(remaining);
        DEBUG_EVENT event{};
        if (!WaitForDebugEvent(&event, waitMs)) {
            if (GetLastError() == ERROR_SEM_TIMEOUT) continue;
            fail();
            break;
        }
        DWORD status = DBG_CONTINUE;
        if (firstEvent) {
            firstEvent = false;
            if (event.dwDebugEventCode != CREATE_PROCESS_DEBUG_EVENT ||
                event.dwProcessId != pi.dwProcessId || event.dwThreadId != pi.dwThreadId) {
                fail();
            } else {
                e.ownerInitialEvent = true;
            }
        }
        switch (event.dwDebugEventCode) {
        case CREATE_PROCESS_DEBUG_EVENT: {
            const CREATE_PROCESS_DEBUG_INFO& info = event.u.CreateProcessInfo;
            if (info.hFile != nullptr && !CloseHandle(info.hFile)) fail();
            if (event.dwProcessId != pi.dwProcessId && childPid == 0) {
                childPid = event.dwProcessId;
                e.childCreateEvent = true;
                std::wstring image;
                e.childPathMatched = info.hProcess != nullptr &&
                    imagePath(info.hProcess, image) && samePath(image, redisPath);
                if (!e.childPathMatched) fail();
                if (info.hProcess != nullptr &&
                    DuplicateHandle(GetCurrentProcess(), info.hProcess, GetCurrentProcess(),
                                    &childSync, 0, FALSE, DUPLICATE_SAME_ACCESS)) {
                    e.childHandleDuplicated = true;
                } else {
                    fail();
                }
                terminateOwner();
            }
            break;
        }
        case LOAD_DLL_DEBUG_EVENT:
            if (event.u.LoadDll.hFile != nullptr && !CloseHandle(event.u.LoadDll.hFile)) fail();
            break;
        case EXCEPTION_DEBUG_EVENT:
            if (event.u.Exception.ExceptionRecord.ExceptionCode != EXCEPTION_BREAKPOINT)
                status = DBG_EXCEPTION_NOT_HANDLED;
            break;
        case EXIT_PROCESS_DEBUG_EVENT:
            if (event.dwProcessId == pi.dwProcessId) {
                e.ownerExitObserved = true;
                e.ownerExitCode = static_cast<int>(event.u.ExitProcess.dwExitCode);
            }
            if (childPid != 0 && event.dwProcessId == childPid) {
                e.childExitObserved = true;
                e.childExitCode = static_cast<int>(event.u.ExitProcess.dwExitCode);
            }
            break;
        default:
            break;
        }
        if (!ContinueDebugEvent(event.dwProcessId, event.dwThreadId, status)) {
            e.allEventsContinued = false;
            fail();
            break;
        }
    }
    if (childSync != nullptr) {
        DWORD state = WaitForSingleObject(childSync, 5000);
        e.childWaitSignaled = state == WAIT_OBJECT_0;
        if (state == WAIT_FAILED) failure = true;
        CloseHandle(childSync);
    }
    if (WaitForSingleObject(pi.hProcess, 5000) != WAIT_OBJECT_0) failure = true;
    CloseHandle(pi.hThread);
    CloseHandle(pi.hProcess);

    bool success = !failure && e.argumentsValid && e.ownerInitialEvent &&
                   e.childCreateEvent && e.childPathMatched && e.childHandleDuplicated &&
                   e.ownerTerminationSucceeded && e.ownerExitObserved &&
                   e.ownerExitCode == 97 && e.childExitObserved && e.childWaitSignaled &&
                   e.allEventsContinued;
    return report(e, success);
}
