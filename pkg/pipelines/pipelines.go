package pipelines

import (
	"fmt"
	"strings"
	"sync"

	"github.com/megaease/easegateway/pkg/task"
)

type PipelineCommonConfig struct {
	Name                              string   `json:"pipeline_name"`
	Plugins                           []string `json:"plugin_names"`
	ParallelismCount                  uint16   `json:"parallelism"`                    // up to 65535, zero means dynamic auto-schedule mode
	CrossPipelineRequestBacklogLength uint16   `json:"cross_pipeline_request_backlog"` // up to 65535
}

func (c *PipelineCommonConfig) PipelineName() string {
	return c.Name
}

func (c *PipelineCommonConfig) PluginNames() []string {
	return c.Plugins
}

func (c *PipelineCommonConfig) Parallelism() uint16 {
	return c.ParallelismCount
}

func (c *PipelineCommonConfig) CrossPipelineRequestBacklog() uint16 {
	return c.CrossPipelineRequestBacklogLength
}

func (c *PipelineCommonConfig) Prepare() error {
	if len(strings.TrimSpace(c.PipelineName())) == 0 {
		return fmt.Errorf("invalid pipeline name")
	}

	if len(c.PluginNames()) == 0 {
		return fmt.Errorf("pipeline is empty")
	}

	if c.CrossPipelineRequestBacklogLength == 0 {
		c.CrossPipelineRequestBacklogLength = 10240
	}

	return nil
}

const DATA_BUCKET_FOR_ALL_PLUGIN_INSTANCE = "*"

type SourceInputQueueLengthGetter func() uint32
type SourceInputTrigger func(getterName string, getter SourceInputQueueLengthGetter)

var NoOpSourceInputTrigger SourceInputTrigger = func(getterName string, getter SourceInputQueueLengthGetter) {}

////

type PipelineContext interface {
	// PipelineName returns pipeline name
	PipelineName() string
	// PluginNames returns sequential plugin names
	PluginNames() []string
	// Statistics returns pipeline statistics
	Statistics() PipelineStatistics
	// DataBucket returns(creates a new one if necessary) pipeline data bucket corresponding with plugin.
	// If the pluginInstanceId doesn't equal to DATA_BUCKET_FOR_ALL_PLUGIN_INSTANCE
	// (usually memory address of the instance), the data bucket should be deleted by the plugin instance's CleanUp().
	// If the pluginInstanceId equals to DATA_BUCKET_FOR_ALL_PLUGIN_INSTANCE, which indicates all instances
	// of a plugin share one data bucket, the data bucket will be deleted automatically the
	// plugin (not the plugin instance) is deleted.
	DataBucket(pluginName, pluginInstanceId string) PipelineContextDataBucket
	// DeleteBucket deletes a data bucket.
	DeleteBucket(pluginName, pluginInstanceId string) PipelineContextDataBucket
	// Downstream pipeline calls PushCrossPipelineRequest to commit a request
	CommitCrossPipelineRequest(request *DownstreamRequest, cancel <-chan struct{}) error
	// Upstream pipeline calls PopCrossPipelineRequest to claim a request
	ClaimCrossPipelineRequest(cancel <-chan struct{}) *DownstreamRequest
	// Upstream pipeline calls CrossPipelineWIPRequestsCount to make sure how many requests are waiting process
	CrossPipelineWIPRequestsCount(upstreamPipelineName string) int
	// Trigger when input of source plugin arrived
	TriggerSourceInput(getterName string, getter SourceInputQueueLengthGetter)
	// Close closes a PipelineContext
	Close()
}

////

type DefaultValueFunc func() interface{}

type PipelineContextDataBucket interface {
	// BindData binds data, the type of key must be comparable
	BindData(key, value interface{}) (interface{}, error)
	// QueryData querys data, return nil if not found
	QueryData(key interface{}) interface{}
	// QueryDataWithBindDefault queries data with binding default data if not found, return final value
	QueryDataWithBindDefault(key interface{}, defaultValueFunc DefaultValueFunc) (interface{}, error)
	// UnbindData unbinds data
	UnbindData(key interface{}) interface{}
}

////

type DownstreamRequest struct {
	upstreamPipelineName, downstreamPipelineName string
	data                                         map[string]interface{}
	responseChanLock                             sync.Mutex
	responseChan                                 chan *UpstreamResponse
}

func NewDownstreamRequest(upstreamPipelineName, downstreamPipelineName string,
	data map[string]interface{}) *DownstreamRequest {

	ret := &DownstreamRequest{
		upstreamPipelineName:   upstreamPipelineName,
		downstreamPipelineName: downstreamPipelineName,
		data:                   data,
		// zero size channel guarantees client front of downstream receives response
		// after backend back of upstream respond successfully
		// there is not any queue on the response path between the client and real backend
		responseChan: make(chan *UpstreamResponse, 0),
	}

	return ret
}

func (r *DownstreamRequest) UpstreamPipelineName() string {
	return r.upstreamPipelineName
}

func (r *DownstreamRequest) DownstreamPipelineName() string {
	return r.downstreamPipelineName
}

func (r *DownstreamRequest) Data() map[string]interface{} {
	return r.data
}

func (r *DownstreamRequest) Respond(response *UpstreamResponse, cancel <-chan struct{}) error {
	if r.responseChan == nil {
		return fmt.Errorf("request from pipeline %s was closed", r.downstreamPipelineName)
	}

	return func() (err error) {
		defer func() {
			// to prevent send on closed channel due to
			// Close() of the downstream request can be called concurrently
			e := recover()
			if e != nil {
				err = fmt.Errorf("request from pipeline %s is closed", r.downstreamPipelineName)
			}
		}()

		select {
		case r.responseChan <- response:
			err = nil
		case <-cancel:
			err = fmt.Errorf("response is canclled")
		}

		return
	}()
}

func (r *DownstreamRequest) Response() <-chan *UpstreamResponse {
	return r.responseChan
}

func (r *DownstreamRequest) Close() {
	r.responseChanLock.Lock()
	defer r.responseChanLock.Unlock()

	if r.responseChan != nil {
		close(r.responseChan)
		r.responseChan = nil
	}
}

type UpstreamResponse struct {
	UpstreamPipelineName string
	Data                 map[string]interface{}
	TaskError            error
	TaskResultCode       task.TaskResultCode
}

////

type StatisticsKind string

const (
	SuccessStatistics StatisticsKind = "SuccessStatistics"
	FailureStatistics StatisticsKind = "FailureStatistics"
	AllStatistics     StatisticsKind = "AllStatistics"

	STATISTICS_INDICATOR_FOR_ALL_PLUGIN_INSTANCE = "*"
)

type StatisticsIndicatorEvaluator func(name, indicatorName string) (interface{}, error)

type PipelineThroughputRateUpdated func(name string, latestStatistics PipelineStatistics)
type PipelineExecutionSampleUpdated func(name string, latestStatistics PipelineStatistics)
type PluginThroughputRateUpdated func(name string, latestStatistics PipelineStatistics, kind StatisticsKind)
type PluginExecutionSampleUpdated func(name string, latestStatistics PipelineStatistics, kind StatisticsKind)

type PipelineStatistics interface {
	PipelineThroughputRate1() (float64, error)
	PipelineThroughputRate5() (float64, error)
	PipelineThroughputRate15() (float64, error)
	PipelineExecutionCount() (int64, error)
	PipelineExecutionTimeMax() (int64, error)
	PipelineExecutionTimeMin() (int64, error)
	PipelineExecutionTimePercentile(percentile float64) (float64, error)
	PipelineExecutionTimeStdDev() (float64, error)
	PipelineExecutionTimeVariance() (float64, error)

	PluginThroughputRate1(pluginName string, kind StatisticsKind) (float64, error)
	PluginThroughputRate5(pluginName string, kind StatisticsKind) (float64, error)
	PluginThroughputRate15(pluginName string, kind StatisticsKind) (float64, error)
	PluginExecutionCount(pluginName string, kind StatisticsKind) (int64, error)
	PluginExecutionTimeMax(pluginName string, kind StatisticsKind) (int64, error)
	PluginExecutionTimeMin(pluginName string, kind StatisticsKind) (int64, error)
	PluginExecutionTimePercentile(
		pluginName string, kind StatisticsKind, percentile float64) (float64, error)
	PluginExecutionTimeStdDev(pluginName string, kind StatisticsKind) (float64, error)
	PluginExecutionTimeVariance(pluginName string, kind StatisticsKind) (float64, error)

	TaskExecutionCount(kind StatisticsKind) (uint64, error)

	PipelineIndicatorNames() []string
	PipelineIndicatorValue(indicatorName string) (interface{}, error)
	PluginIndicatorNames(pluginName string) []string
	PluginIndicatorValue(pluginName, indicatorName string) (interface{}, error)
	TaskIndicatorNames() []string
	TaskIndicatorValue(indicatorName string) (interface{}, error)

	AddPipelineThroughputRateUpdatedCallback(name string, callback PipelineThroughputRateUpdated)
	DeletePipelineThroughputRateUpdatedCallback(name string)
	AddPipelineExecutionSampleUpdatedCallback(name string, callback PipelineExecutionSampleUpdated)
	DeletePipelineExecutionSampleUpdatedCallback(name string)
	AddPluginThroughputRateUpdatedCallback(name string, callback PluginThroughputRateUpdated)
	DeletePluginThroughputRateUpdatedCallback(name string)
	AddPluginExecutionSampleUpdatedCallback(name string, callback PluginExecutionSampleUpdated)
	DeletePluginExecutionSampleUpdatedCallback(name string)

	RegisterPluginIndicator(pluginName, pluginInstanceId, indicatorName, desc string,
		evaluator StatisticsIndicatorEvaluator) (bool, error)
	UnregisterPluginIndicator(pluginName, pluginInstanceId, indicatorName string)
}

type Pipeline interface {
	Name() string
	Prepare()
	Run() error
	Stop(scheduled bool)
	Close()
}

////

type Config interface {
	PipelineName() string
	PluginNames() []string
	Parallelism() uint16
	CrossPipelineRequestBacklog() uint16
	Prepare() error
}

// Pipelines register authority

var (
	PIPELINE_TYPES = map[string]interface{}{
		"LinearPipeline": nil,
	}
)

func ValidType(t string) bool {
	_, exists := PIPELINE_TYPES[t]
	return exists
}

func GetAllTypes() []string {
	types := make([]string, 0)
	for t := range PIPELINE_TYPES {
		types = append(types, t)
	}
	return types
}