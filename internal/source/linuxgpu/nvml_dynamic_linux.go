//go:build linux && cgo

package linuxgpu

/*
#cgo LDFLAGS: -ldl

#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef int nvmlReturn_t;
typedef void* nvmlDevice_t;
typedef struct { unsigned int gpu; unsigned int memory; } nvmlUtilization_t;
typedef struct { unsigned long long total; unsigned long long free; unsigned long long used; } nvmlMemory_t;
typedef struct {
    unsigned int version;
    unsigned long long total;
    unsigned long long reserved;
    unsigned long long free;
    unsigned long long used;
} nvmlMemory_v2_t;

typedef nvmlReturn_t (*init_fn)(void);
typedef nvmlReturn_t (*count_fn)(unsigned int*);
typedef nvmlReturn_t (*device_fn)(unsigned int, nvmlDevice_t*);
typedef nvmlReturn_t (*util_fn)(nvmlDevice_t, nvmlUtilization_t*);
typedef nvmlReturn_t (*clock_fn)(nvmlDevice_t, unsigned int, unsigned int*);
typedef nvmlReturn_t (*temp_fn)(nvmlDevice_t, unsigned int, unsigned int*);
typedef nvmlReturn_t (*power_fn)(nvmlDevice_t, unsigned int*);
typedef nvmlReturn_t (*memory_fn)(nvmlDevice_t, nvmlMemory_t*);
typedef nvmlReturn_t (*memory_v2_fn)(nvmlDevice_t, nvmlMemory_v2_t*);
typedef const char* (*error_fn)(nvmlReturn_t);

typedef struct stash_nvml_api {
    void* library;
    init_fn init;
    count_fn count;
    device_fn device;
    util_fn utilization;
    clock_fn clock;
    temp_fn temperature;
    power_fn power;
    memory_fn memory;
    memory_v2_fn memory_v2;
    error_fn error_string;
    char open_error[512];
} stash_nvml_api;

static void* stash_symbol(void* library, const char* preferred, const char* fallback) {
    void* symbol = dlsym(library, preferred);
    if (symbol == NULL && fallback != NULL) {
        symbol = dlsym(library, fallback);
    }
    return symbol;
}

static stash_nvml_api* stash_nvml_open(void) {
    stash_nvml_api* api = (stash_nvml_api*)calloc(1, sizeof(stash_nvml_api));
    if (api == NULL) {
        return NULL;
    }
    api->library = dlopen("libnvidia-ml.so.1", RTLD_NOW | RTLD_LOCAL);
    if (api->library == NULL) {
        api->library = dlopen("libnvidia-ml.so", RTLD_NOW | RTLD_LOCAL);
    }
    if (api->library == NULL) {
        const char* message = dlerror();
        snprintf(api->open_error, sizeof(api->open_error), "%s", message == NULL ? "library not found" : message);
        return api;
    }
    api->init = (init_fn)stash_symbol(api->library, "nvmlInit_v2", "nvmlInit");
    api->count = (count_fn)stash_symbol(api->library, "nvmlDeviceGetCount_v2", "nvmlDeviceGetCount");
    api->device = (device_fn)stash_symbol(api->library, "nvmlDeviceGetHandleByIndex_v2", "nvmlDeviceGetHandleByIndex");
    api->utilization = (util_fn)dlsym(api->library, "nvmlDeviceGetUtilizationRates");
    api->clock = (clock_fn)dlsym(api->library, "nvmlDeviceGetClockInfo");
    api->temperature = (temp_fn)dlsym(api->library, "nvmlDeviceGetTemperature");
    api->power = (power_fn)dlsym(api->library, "nvmlDeviceGetPowerUsage");
    api->memory = (memory_fn)dlsym(api->library, "nvmlDeviceGetMemoryInfo");
    api->memory_v2 = (memory_v2_fn)dlsym(api->library, "nvmlDeviceGetMemoryInfo_v2");
    api->error_string = (error_fn)dlsym(api->library, "nvmlErrorString");
    return api;
}

static int stash_nvml_ready(stash_nvml_api* api) { return api != NULL && api->library != NULL; }
static const char* stash_nvml_open_error(stash_nvml_api* api) { return api == NULL ? "allocation failed" : api->open_error; }
static void stash_nvml_close(stash_nvml_api* api) {
    if (api == NULL) return;
    if (api->library != NULL) dlclose(api->library);
    free(api);
}
static const char* stash_nvml_error(stash_nvml_api* api, int code) {
    if (api != NULL && api->error_string != NULL) return api->error_string(code);
    return "NVML call failed";
}

enum { STASH_NVML_MISSING_SYMBOL = -1, STASH_NVML_CLOCK_GRAPHICS = 0, STASH_NVML_TEMPERATURE_GPU = 0 };

static int stash_nvml_init(stash_nvml_api* api) {
    return api == NULL || api->init == NULL ? STASH_NVML_MISSING_SYMBOL : api->init();
}
static int stash_nvml_count(stash_nvml_api* api, unsigned int* value) {
    return api == NULL || api->count == NULL ? STASH_NVML_MISSING_SYMBOL : api->count(value);
}
static int stash_nvml_device(stash_nvml_api* api, unsigned int index, nvmlDevice_t* value) {
    return api == NULL || api->device == NULL ? STASH_NVML_MISSING_SYMBOL : api->device(index, value);
}
static int stash_nvml_usage(stash_nvml_api* api, nvmlDevice_t device, unsigned int* value) {
    if (api == NULL || api->utilization == NULL) return STASH_NVML_MISSING_SYMBOL;
    nvmlUtilization_t rates;
    int result = api->utilization(device, &rates);
    if (result == 0) *value = rates.gpu;
    return result;
}
static int stash_nvml_clock(stash_nvml_api* api, nvmlDevice_t device, unsigned int* value) {
    return api == NULL || api->clock == NULL ? STASH_NVML_MISSING_SYMBOL : api->clock(device, STASH_NVML_CLOCK_GRAPHICS, value);
}
static int stash_nvml_temperature(stash_nvml_api* api, nvmlDevice_t device, unsigned int* value) {
    return api == NULL || api->temperature == NULL ? STASH_NVML_MISSING_SYMBOL : api->temperature(device, STASH_NVML_TEMPERATURE_GPU, value);
}
static int stash_nvml_power(stash_nvml_api* api, nvmlDevice_t device, unsigned int* value) {
    return api == NULL || api->power == NULL ? STASH_NVML_MISSING_SYMBOL : api->power(device, value);
}
static int stash_nvml_memory(stash_nvml_api* api, nvmlDevice_t device, unsigned long long* used, unsigned long long* total) {
    if (api == NULL) return STASH_NVML_MISSING_SYMBOL;
    if (api->memory_v2 != NULL) {
        nvmlMemory_v2_t memory;
        memset(&memory, 0, sizeof(memory));
        memory.version = ((unsigned int)sizeof(memory)) | (2U << 24);
        int result = api->memory_v2(device, &memory);
        if (result == 0) {
            *used = memory.used;
            *total = memory.total;
        }
        return result;
    }
    if (api->memory == NULL) return STASH_NVML_MISSING_SYMBOL;
    nvmlMemory_t memory;
    memset(&memory, 0, sizeof(memory));
    int result = api->memory(device, &memory);
    if (result == 0) {
        *used = memory.used;
        *total = memory.total;
    }
    return result;
}
*/
import "C"

import (
	"fmt"
)

type dynamicNVML struct {
	api *C.stash_nvml_api
}

type dynamicNVMLDevice struct {
	client *dynamicNVML
	handle C.nvmlDevice_t
}

func loadDynamicNVML() (nvmlClient, error) {
	api := C.stash_nvml_open()
	if api == nil {
		return nil, fmt.Errorf("open NVML: allocation failed")
	}
	if C.stash_nvml_ready(api) == 0 {
		err := fmt.Errorf("open NVML: %s", C.GoString(C.stash_nvml_open_error(api)))
		C.stash_nvml_close(api)
		return nil, err
	}
	client := &dynamicNVML{api: api}
	if code := int(C.stash_nvml_init(api)); code != 0 {
		err := client.callError("initialize NVML", code)
		C.stash_nvml_close(api)
		return nil, err
	}
	// The client intentionally stays initialized for the registry lifetime.
	// Collector closures retain it, and process teardown releases NVML safely.
	return client, nil
}

func (client *dynamicNVML) callError(operation string, code int) error {
	if code == -1 {
		return fmt.Errorf("%s: required NVML function is unavailable", operation)
	}
	return fmt.Errorf("%s: %s (code %d)", operation, C.GoString(C.stash_nvml_error(client.api, C.int(code))), code)
}

func (client *dynamicNVML) DeviceCount() (uint32, error) {
	var value C.uint
	if code := int(C.stash_nvml_count(client.api, &value)); code != 0 {
		return 0, client.callError("query device count", code)
	}
	return uint32(value), nil
}

func (client *dynamicNVML) Device(index uint32) (nvmlDevice, error) {
	var handle C.nvmlDevice_t
	if code := int(C.stash_nvml_device(client.api, C.uint(index), &handle)); code != 0 {
		return nil, client.callError(fmt.Sprintf("open device %d", index), code)
	}
	if handle == nil {
		return nil, fmt.Errorf("open device %d: NVML returned a nil handle", index)
	}
	return &dynamicNVMLDevice{client: client, handle: handle}, nil
}

func (device *dynamicNVMLDevice) UsagePercent() (uint32, error) {
	var value C.uint
	if code := int(C.stash_nvml_usage(device.client.api, device.handle, &value)); code != 0 {
		return 0, device.client.callError("query utilization", code)
	}
	return uint32(value), nil
}

func (device *dynamicNVMLDevice) GraphicsClockMHz() (uint32, error) {
	var value C.uint
	if code := int(C.stash_nvml_clock(device.client.api, device.handle, &value)); code != 0 {
		return 0, device.client.callError("query graphics clock", code)
	}
	return uint32(value), nil
}

func (device *dynamicNVMLDevice) TemperatureC() (uint32, error) {
	var value C.uint
	if code := int(C.stash_nvml_temperature(device.client.api, device.handle, &value)); code != 0 {
		return 0, device.client.callError("query temperature", code)
	}
	return uint32(value), nil
}

func (device *dynamicNVMLDevice) PowerMilliwatts() (uint32, error) {
	var value C.uint
	if code := int(C.stash_nvml_power(device.client.api, device.handle, &value)); code != 0 {
		return 0, device.client.callError("query power draw", code)
	}
	return uint32(value), nil
}

func (device *dynamicNVMLDevice) MemoryBytes() (uint64, uint64, error) {
	var used, total C.ulonglong
	if code := int(C.stash_nvml_memory(device.client.api, device.handle, &used, &total)); code != 0 {
		return 0, 0, device.client.callError("query memory", code)
	}
	return uint64(used), uint64(total), nil
}
