'use client';

import { PortfolioMetrics } from '@/lib/api';
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js';
import { Pie } from 'react-chartjs-2';

ChartJS.register(ArcElement, Tooltip, Legend);

interface PortfolioSummaryProps {
  metrics: PortfolioMetrics;
}

export default function PortfolioSummary({ metrics }: PortfolioSummaryProps) {
  const formatCurrency = (num: number) => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
    }).format(num);
  };

  const formatPercent = (num: number, decimals: number = 2) => {
    return `${num?.toFixed(decimals) || '0.00'}%`;
  };

  // Prepare chart data
  const sectorWeights = metrics.sector_weights || {};
  const chartData = {
    labels: Object.keys(sectorWeights),
    datasets: [
      {
        data: Object.values(sectorWeights),
        backgroundColor: [
          'rgba(59, 130, 246, 0.8)', // blue
          'rgba(16, 185, 129, 0.8)', // green
          'rgba(245, 158, 11, 0.8)', // amber
          'rgba(239, 68, 68, 0.8)',  // red
          'rgba(139, 92, 246, 0.8)', // purple
          'rgba(236, 72, 153, 0.8)', // pink
          'rgba(20, 184, 166, 0.8)', // teal
        ],
        borderColor: 'rgba(31, 41, 55, 1)',
        borderWidth: 2,
      },
    ],
  };

  const chartOptions = {
    plugins: {
      legend: {
        position: 'bottom' as const,
        labels: {
          color: 'rgb(209, 213, 219)',
          padding: 15,
        },
      },
      tooltip: {
        callbacks: {
          label: function(context: any) {
            return `${context.label}: ${context.parsed.toFixed(1)}%`;
          }
        }
      }
    },
    maintainAspectRatio: false,
  };

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
      {/* Total Portfolio Value */}
      <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
        <h3 className="text-sm font-medium text-gray-400 mb-2">
          Total Portfolio Value
        </h3>
        <p className="text-2xl font-bold text-white">
          {formatCurrency(metrics.total_value || 0)}
        </p>
      </div>

      {/* Overall EV */}
      <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
        <h3 className="text-sm font-medium text-gray-400 mb-2 tooltip">
          Overall Expected Value
          <span className="tooltiptext">Weighted average EV across all positions</span>
        </h3>
        <p className={`text-2xl font-bold ${
          (metrics.overall_ev || 0) > 7 ? 'text-green-400' : 
          (metrics.overall_ev || 0) > 0 ? 'text-yellow-400' : 
          'text-red-400'
        }`}>
          {formatPercent(metrics.overall_ev || 0, 1)}
        </p>
      </div>

      {/* Sharpe Ratio */}
      <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
        <h3 className="text-sm font-medium text-gray-400 mb-2 tooltip">
          Sharpe Ratio
          <span className="tooltiptext">Risk-adjusted return: EV / Volatility</span>
        </h3>
        <p className="text-2xl font-bold text-white">
          {(metrics.sharpe_ratio || 0).toFixed(2)}
        </p>
      </div>

      {/* Volatility */}
      <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
        <h3 className="text-sm font-medium text-gray-400 mb-2 tooltip">
          Portfolio Volatility
          <span className="tooltiptext">Weighted average volatility (target: 11-13%)</span>
        </h3>
        <p className={`text-2xl font-bold ${
          (metrics.weighted_volatility || 0) > 13 ? 'text-red-400' : 
          (metrics.weighted_volatility || 0) < 11 ? 'text-yellow-400' : 
          'text-green-400'
        }`}>
          {formatPercent(metrics.weighted_volatility || 0, 1)}
        </p>
      </div>

      {/* Kelly Utilization */}
      <div className="bg-gray-800 rounded-lg p-6 border border-gray-700">
        <h3 className="text-sm font-medium text-gray-400 mb-2 tooltip">
          Kelly Utilization
          <span className="tooltiptext">Sum of position weights vs. suggested allocations</span>
        </h3>
        <p className="text-2xl font-bold text-white">
          {formatPercent(metrics.kelly_utilization || 0, 1)}
        </p>
      </div>

      {/* Sector Allocation Chart */}
      <div className="bg-gray-800 rounded-lg p-6 border border-gray-700 col-span-1 md:col-span-2 lg:col-span-3">
        <h3 className="text-sm font-medium text-gray-400 mb-4">
          Sector Allocation
        </h3>
        <div className="h-64">
          <Pie data={chartData} options={chartOptions} />
        </div>
      </div>
    </div>
  );
}

