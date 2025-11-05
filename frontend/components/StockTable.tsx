'use client';

import { useState } from 'react';
import Link from 'next/link';
import { Stock } from '@/lib/api';
import { TrashIcon, ArrowPathIcon } from '@heroicons/react/24/outline';

interface StockTableProps {
  stocks: Stock[];
  onDelete: (id: number) => void;
  onUpdate: (id: number) => void;
}

export default function StockTable({ stocks, onDelete, onUpdate }: StockTableProps) {
  const [sortField, setSortField] = useState<keyof Stock>('ticker');
  const [sortDirection, setSortDirection] = useState<'asc' | 'desc'>('asc');
  const [filter, setFilter] = useState('');

  const handleSort = (field: keyof Stock) => {
    if (sortField === field) {
      setSortDirection(sortDirection === 'asc' ? 'desc' : 'asc');
    } else {
      setSortField(field);
      setSortDirection('asc');
    }
  };

  const filteredStocks = (stocks || []).filter(
    (stock) =>
      stock.ticker.toLowerCase().includes(filter.toLowerCase()) ||
      stock.company_name.toLowerCase().includes(filter.toLowerCase()) ||
      stock.sector.toLowerCase().includes(filter.toLowerCase())
  );

  const sortedStocks = [...filteredStocks].sort((a, b) => {
    const aVal = a[sortField];
    const bVal = b[sortField];
    
    if (typeof aVal === 'string' && typeof bVal === 'string') {
      return sortDirection === 'asc' 
        ? aVal.localeCompare(bVal)
        : bVal.localeCompare(aVal);
    }
    
    if (typeof aVal === 'number' && typeof bVal === 'number') {
      return sortDirection === 'asc' ? aVal - bVal : bVal - aVal;
    }
    
    return 0;
  });

  const getRowClass = (assessment: string) => {
    switch (assessment.toLowerCase()) {
      case 'add':
        return 'assessment-add hover:bg-green-900 hover:bg-opacity-20';
      case 'trim':
        return 'assessment-trim hover:bg-orange-900 hover:bg-opacity-20';
      case 'sell':
        return 'assessment-sell hover:bg-red-900 hover:bg-opacity-20';
      default:
        return 'assessment-hold hover:bg-gray-800';
    }
  };

  const formatNumber = (num: number, decimals: number = 2) => {
    return num?.toFixed(decimals) || '0.00';
  };

  const formatCurrency = (num: number) => {
    return `$${formatNumber(num, 2)}`;
  };

  return (
    <div className="stock-table">
      <div className="mb-4">
        <input
          type="text"
          placeholder="Search by ticker, company, or sector..."
          className="w-full px-4 py-2 bg-gray-800 border border-gray-700 rounded-lg text-white placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-primary-500"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>

      <div className="overflow-x-auto rounded-lg border border-gray-700">
        <table className="min-w-full divide-y divide-gray-700">
          <thead className="bg-gray-800">
            <tr>
              <th
                className="px-3 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700"
                onClick={() => handleSort('ticker')}
              >
                Ticker
              </th>
              <th
                className="px-3 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700"
                onClick={() => handleSort('company_name')}
              >
                Company
              </th>
              <th
                className="px-3 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700"
                onClick={() => handleSort('sector')}
              >
                Sector
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700 tooltip"
                onClick={() => handleSort('current_price')}
              >
                Price
                <span className="tooltiptext">Current market price in local currency</span>
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700 tooltip"
                onClick={() => handleSort('fair_value')}
              >
                Fair Value
                <span className="tooltiptext">Consensus analyst target price</span>
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700 tooltip"
                onClick={() => handleSort('upside_potential')}
              >
                Upside %
                <span className="tooltiptext">Potential gain to fair value: ((FV - Price) / Price) × 100</span>
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700 tooltip"
                onClick={() => handleSort('expected_value')}
              >
                EV %
                <span className="tooltiptext">Expected Value: (p × Upside) + ((1-p) × Downside)</span>
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700 tooltip"
                onClick={() => handleSort('kelly_fraction')}
              >
                Kelly f* %
                <span className="tooltiptext">Optimal position size: ((b×p) - (1-p)) / b</span>
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700 tooltip"
                onClick={() => handleSort('half_kelly_suggested')}
              >
                ½-Kelly %
                <span className="tooltiptext">Conservative position size (capped at 15%)</span>
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700"
                onClick={() => handleSort('shares_owned')}
              >
                Shares
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700"
                onClick={() => handleSort('weight')}
              >
                Weight %
              </th>
              <th
                className="px-3 py-3 text-right text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700"
                onClick={() => handleSort('unrealized_pnl')}
              >
                P&L
              </th>
              <th
                className="px-3 py-3 text-center text-xs font-medium text-gray-300 uppercase tracking-wider cursor-pointer hover:bg-gray-700"
                onClick={() => handleSort('assessment')}
              >
                Assessment
              </th>
              <th className="px-3 py-3 text-center text-xs font-medium text-gray-300 uppercase tracking-wider">
                Actions
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-700">
            {sortedStocks.map((stock) => (
              <tr key={stock.id} className={getRowClass(stock.assessment)}>
                <td className="px-3 py-4 whitespace-nowrap text-sm font-medium text-primary-400">
                  {stock.ticker}
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm">
                  <Link
                    href={`/stocks/${stock.id}`}
                    className="text-primary-400 hover:text-primary-300 hover:underline"
                  >
                    {stock.company_name}
                  </Link>
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm">
                  {stock.sector}
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm text-right">
                  {formatNumber(stock.current_price)} {stock.currency}
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm text-right">
                  {formatNumber(stock.fair_value)} {stock.currency}
                </td>
                <td className={`px-3 py-4 whitespace-nowrap text-sm text-right font-semibold ${
                  stock.upside_potential > 0 ? 'text-green-400' : 'text-red-400'
                }`}>
                  {formatNumber(stock.upside_potential, 1)}%
                </td>
                <td className={`px-3 py-4 whitespace-nowrap text-sm text-right font-semibold ${
                  stock.expected_value > 7 ? 'text-green-400' : 
                  stock.expected_value > 0 ? 'text-yellow-400' : 'text-red-400'
                }`}>
                  {formatNumber(stock.expected_value, 1)}%
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm text-right">
                  {formatNumber(stock.kelly_fraction, 1)}%
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm text-right">
                  {formatNumber(stock.half_kelly_suggested, 1)}%
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm text-right">
                  {stock.shares_owned}
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm text-right">
                  {formatNumber(stock.weight, 1)}%
                </td>
                <td className={`px-3 py-4 whitespace-nowrap text-sm text-right font-semibold ${
                  stock.unrealized_pnl >= 0 ? 'text-green-400' : 'text-red-400'
                }`}>
                  {formatCurrency(stock.unrealized_pnl)}
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm text-center">
                  <span className={`px-2 py-1 rounded-full text-xs font-semibold ${
                    stock.assessment === 'Add' ? 'bg-green-900 text-green-200' :
                    stock.assessment === 'Hold' ? 'bg-gray-700 text-gray-200' :
                    stock.assessment === 'Trim' ? 'bg-orange-900 text-orange-200' :
                    'bg-red-900 text-red-200'
                  }`}>
                    {stock.assessment}
                  </span>
                </td>
                <td className="px-3 py-4 whitespace-nowrap text-sm text-center">
                  <div className="flex justify-center space-x-2">
                    <button
                      onClick={() => onUpdate(stock.id)}
                      className="text-primary-400 hover:text-primary-300 transition-colors"
                      title="Update stock data"
                    >
                      <ArrowPathIcon className="h-5 w-5" />
                    </button>
                    <button
                      onClick={() => onDelete(stock.id)}
                      className="text-red-400 hover:text-red-300 transition-colors"
                      title="Delete stock"
                    >
                      <TrashIcon className="h-5 w-5" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {sortedStocks.length === 0 && (
        <div className="text-center py-12 text-gray-400">
          <p>No stocks found. Add your first stock to get started.</p>
        </div>
      )}
    </div>
  );
}

