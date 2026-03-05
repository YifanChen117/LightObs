package main

import (
	"flag"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	api "lightobs/internal/operator/api/v1alpha1"
	"lightobs/internal/operator/controller"
	"lightobs/internal/operator/reconciler"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	// 注册内置 K8s 类型（Pod、Namespace 等）
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	// 注册 CRD 类型（PodObservation）
	utilruntime.Must(api.AddToScheme(scheme))
	_ = corev1.AddToScheme(scheme) // 显示注册，确保 Watch Pod 正常工作
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		leaderElectionEnable bool
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8090", "Operator metrics 监听地址")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8091", "Operator 健康检查监听地址")
	flag.BoolVar(&leaderElectionEnable, "leader-elect", false,
		"是否启用 Leader Election（多副本部署时开启）")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElectionEnable,
		// LeaderElectionID 需全局唯一，防止与其他 Operator 冲突
		LeaderElectionID: "lightobs-operator.lightobs.io",
	})
	if err != nil {
		setupLog.Error(err, "初始化 Manager 失败")
		os.Exit(1)
	}

	// 构造 Reconciler 并注册到 Manager
	rec := &controller.PodObservationReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		AgentManager:  reconciler.NewAgentManager(mgr.GetClient()),
		StatusUpdater: reconciler.NewStatusUpdater(mgr.GetClient()),
	}
	if err := rec.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "注册 Controller 失败")
		os.Exit(1)
	}

	// 健康检查端点（Liveness / Readiness）
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "注册 healthz 失败")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "注册 readyz 失败")
		os.Exit(1)
	}

	setupLog.Info("启动 LightObs Operator")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Operator 运行异常退出")
		os.Exit(1)
	}
}
