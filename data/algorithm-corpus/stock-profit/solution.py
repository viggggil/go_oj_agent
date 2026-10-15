n=int(input()); a=list(map(int,input().split())); low=a[0]; ans=0
for x in a:
    ans=max(ans,x-low); low=min(low,x)
print(ans)
